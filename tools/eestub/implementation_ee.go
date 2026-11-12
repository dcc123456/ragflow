//
//  Copyright (c) 2026 InfiniFlow. All rights reserved.
//
//  RAGFlow Enterprise Edition — Proprietary and Confidential.
//
//  This file is part of the RAGFlow Enterprise Edition and is licensed,
//  not sold, under the RAGFlow Enterprise Commercial License Agreement.
//  Unauthorized copying, modification, distribution, or disclosure of this
//  file, via any medium, is strictly prohibited.
//
//  Portions of this file may incorporate code from RAGFlow Community Edition,
//  which is licensed under the Apache License, Version 2.0.
//
//  For commercial licensing inquiries: sales@infiniflow.ai
//

package main

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strconv"
	"strings"
)

type implementationState uint8

const (
	uncertainImplementation implementationState = iota
	placeholderImplementation
	concreteImplementation
)

// This recognizes syntactic hook defaults, not business correctness. Ambiguous
// bodies are reviewed as warnings and must never trigger automatic generation.
type implementationClassifier struct {
	functions map[string]declaration
	cache     map[*ast.FuncDecl]implementationState
	visiting  map[*ast.FuncDecl]bool
	types     map[string]bool
}

func newImplementationClassifier(files []*sourceFile) *implementationClassifier {
	c := &implementationClassifier{functions: map[string]declaration{}, cache: map[*ast.FuncDecl]implementationState{}, visiting: map[*ast.FuncDecl]bool{}, types: map[string]bool{}}
	for _, file := range files {
		if !file.community {
			continue
		}
		for _, decl := range file.file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil {
				c.functions[fn.Name.Name] = declaration{file, fn}
			}
			if gen, ok := decl.(*ast.GenDecl); ok {
				for _, spec := range gen.Specs {
					if typ, ok := spec.(*ast.TypeSpec); ok {
						c.types[typ.Name.Name] = true
					}
				}
			}
		}
	}
	return c
}

func mergeImplementation(a, b implementationState) implementationState {
	if a == concreteImplementation || b == concreteImplementation {
		return concreteImplementation
	}
	if a == uncertainImplementation || b == uncertainImplementation {
		return uncertainImplementation
	}
	return placeholderImplementation
}

func (c *implementationClassifier) classify(fn *ast.FuncDecl, file *sourceFile) implementationState {
	if value, ok := c.cache[fn]; ok {
		return value
	}
	if fn.Body == nil || c.visiting[fn] {
		return uncertainImplementation
	}
	c.visiting[fn] = true
	params := map[string]bool{}
	for _, fields := range []*ast.FieldList{fn.Recv, fn.Type.Params} {
		if fields != nil {
			for _, field := range fields.List {
				for _, name := range field.Names {
					params[name.Name] = true
				}
			}
		}
	}
	value := c.block(fn.Body, file, params)
	delete(c.visiting, fn)
	c.cache[fn] = value
	return value
}

func (c *implementationClassifier) block(body *ast.BlockStmt, file *sourceFile, params map[string]bool) implementationState {
	result := placeholderImplementation
	for _, stmt := range body.List {
		value := uncertainImplementation
		switch stmt := stmt.(type) {
		case *ast.EmptyStmt:
			value = placeholderImplementation
		case *ast.ReturnStmt:
			value = placeholderImplementation
			for _, expr := range stmt.Results {
				value = mergeImplementation(value, c.expression(expr, file, params))
			}
		case *ast.ExprStmt:
			value = c.expression(stmt.X, file, params)
		case *ast.BlockStmt:
			value = c.block(stmt, file, params)
		default:
			// Control flow and local assignments alone do not prove that a hook
			// is implemented. Actual calls, computation or state writes do.
			ast.Inspect(stmt, func(node ast.Node) bool {
				switch node := node.(type) {
				case *ast.CallExpr:
					value = mergeImplementation(value, c.expression(node, file, params))
					return false
				case *ast.ReturnStmt:
					for _, expr := range node.Results {
						value = mergeImplementation(value, c.expression(expr, file, params))
					}
					return false
				case *ast.AssignStmt:
					for _, lhs := range node.Lhs {
						switch lhs := lhs.(type) {
						case *ast.SelectorExpr, *ast.IndexExpr, *ast.StarExpr:
							value = concreteImplementation
						case *ast.Ident:
							if lhs.Name != "_" && !params[lhs.Name] && node.Tok == token.ASSIGN && (lhs.Obj == nil || file.file.Scope.Objects[lhs.Name] == lhs.Obj) {
								value = concreteImplementation
							}
						}
					}
				case *ast.SendStmt:
					value = concreteImplementation
				}
				return true
			})
		}
		result = mergeImplementation(result, value)
	}
	return result
}

func (c *implementationClassifier) expression(expr ast.Expr, file *sourceFile, params map[string]bool) implementationState {
	switch expr := expr.(type) {
	case *ast.Ident:
		if params[expr.Name] || expr.Obj == nil && (expr.Name == "nil" || expr.Name == "false") {
			return placeholderImplementation
		}
	case *ast.BasicLit:
		value := constant.MakeFromLiteral(expr.Value, expr.Kind, 0)
		if value.Kind() == constant.String {
			if constant.StringVal(value) == "" {
				return placeholderImplementation
			}
		} else if value.Kind() != constant.Unknown && constant.Compare(value, token.EQL, constant.MakeInt64(0)) {
			return placeholderImplementation
		}
	case *ast.ParenExpr:
		return c.expression(expr.X, file, params)
	case *ast.CompositeLit:
		if len(expr.Elts) == 0 {
			return placeholderImplementation
		}
	case *ast.UnaryExpr:
		_, aggregate := expr.X.(*ast.CompositeLit)
		_, literal := expr.X.(*ast.BasicLit)
		if expr.Op == token.AND && aggregate {
			return concreteImplementation
		}
		if (expr.Op == token.ADD || expr.Op == token.SUB) && literal {
			return c.expression(expr.X, file, params)
		}
		return concreteImplementation
	case *ast.BinaryExpr:
		return concreteImplementation
	case *ast.SelectorExpr:
		if c.commonConstant(expr, file, "CodeSuccess") || c.commonConstant(expr, file, "CodeNotImplemented") {
			return placeholderImplementation
		}
		return uncertainImplementation
	case *ast.FuncLit:
		innerParams := make(map[string]bool, len(params))
		for name := range params {
			innerParams[name] = true
		}
		for _, field := range expr.Type.Params.List {
			for _, name := range field.Names {
				innerParams[name.Name] = true
			}
		}
		return c.block(expr.Body, file, innerParams)
	case *ast.CallExpr:
		// A no-op helper can still receive arguments with observable work.
		// Do not erase that work when classifying the enclosing hook.
		argumentWork := placeholderImplementation
		for _, arg := range expr.Args {
			ast.Inspect(arg, func(node ast.Node) bool {
				if _, ok := node.(*ast.FuncLit); ok {
					return false
				}
				if call, ok := node.(*ast.CallExpr); ok {
					argumentWork = mergeImplementation(argumentWork, c.expression(call, file, params))
					return false
				}
				return true
			})
		}
		if argumentWork != placeholderImplementation {
			return argumentWork
		}
		if id, ok := expr.Fun.(*ast.Ident); ok {
			if helper, exists := c.functions[id.Name]; exists && (id.Obj == nil || id.Obj.Kind == ast.Fun) {
				return c.classify(helper.fn, helper.file)
			}
			if id.Name == "panic" && id.Obj == nil {
				if len(expr.Args) == 1 {
					if literal, ok := expr.Args[0].(*ast.BasicLit); ok && literal.Kind == token.STRING {
						message, _ := strconv.Unquote(literal.Value)
						if strings.Contains(strings.ToLower(message), "not implemented") || strings.HasPrefix(message, "missing enterprise implementation:") {
							return placeholderImplementation
						}
					}
				}
				return uncertainImplementation
			}
			_, builtinType := types.Universe.Lookup(id.Name).(*types.TypeName)
			if (builtinType || c.types[id.Name]) && (id.Obj == nil || id.Obj.Kind == ast.Typ) && len(expr.Args) == 1 {
				return c.expression(expr.Args[0], file, params)
			}
			if id.Name == "make" && id.Obj == nil && len(expr.Args) >= 1 {
				switch expr.Args[0].(type) {
				case *ast.ArrayType, *ast.MapType:
					for _, size := range expr.Args[1:] {
						if _, literal := size.(*ast.BasicLit); !literal || c.expression(size, file, params) != placeholderImplementation {
							return uncertainImplementation
						}
					}
					return placeholderImplementation
				}
			}
		}
		if selector, ok := expr.Fun.(*ast.SelectorExpr); ok && c.commonCall(selector, file, "RespondError") {
			for _, arg := range expr.Args {
				if selector, ok := arg.(*ast.SelectorExpr); ok && c.commonConstant(selector, file, "CodeNotImplemented") {
					return placeholderImplementation
				}
			}
		}
		return concreteImplementation
	}
	return uncertainImplementation
}

func (c *implementationClassifier) commonCall(selector *ast.SelectorExpr, file *sourceFile, name string) bool {
	id, ok := selector.X.(*ast.Ident)
	if !ok || selector.Sel.Name != name {
		return false
	}
	spec := file.imports[id.Name]
	if spec == nil {
		return false
	}
	importPath, _ := strconv.Unquote(spec.Path.Value)
	return importPath == "ragflow/internal/common"
}

func (c *implementationClassifier) commonConstant(selector *ast.SelectorExpr, file *sourceFile, name string) bool {
	return c.commonCall(selector, file, name)
}

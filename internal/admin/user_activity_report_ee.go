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

//go:build ee

package admin

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"ragflow/internal/common"
	"ragflow/internal/dao"

	"github.com/gin-gonic/gin"
)

type userActivityReportOptions struct {
	inactive bool
	plan     string
	days     int
	offset   int
	limit    int
}

// ListUsersReports handles login inactivity and recent users of a plan.
func (h *Handler) ListUsersReports(c *gin.Context) {
	opts, err := parseUserActivityReportOptions(c.Request.URL.Query())
	if err != nil {
		common.RespondError(c, common.CodeBadRequest, err.Error())
		return
	}
	rows, total, err := h.service.listUserActivityReport(c.Request.Context(), opts)
	if err != nil {
		common.RespondError(c, common.CodeServerError, err.Error())
		return
	}
	common.RespondSuccessList(c, rows, "", total)
}

func parseUserActivityReportOptions(query url.Values) (userActivityReportOptions, error) {
	opts := userActivityReportOptions{plan: strings.TrimSpace(query.Get("plan")), days: 30, limit: 30}
	for name, values := range query {
		if len(values) != 1 || strings.TrimSpace(values[0]) == "" {
			return opts, fmt.Errorf("%s must have one nonempty value", name)
		}
		switch name {
		case "status", "plan", "days", "page", "page_size":
		default:
			return opts, fmt.Errorf("unsupported activity report parameter %q", name)
		}
	}
	status := strings.ToLower(strings.TrimSpace(query.Get("status")))
	if status != "" && status != "active" && status != "inactive" {
		return opts, fmt.Errorf("status must be active or inactive")
	}
	opts.inactive = status == "inactive"
	if opts.inactive {
		opts.days, opts.limit = 90, 0
	} else if opts.plan == "" {
		return opts, fmt.Errorf("specify status=inactive or a plan for the activity report")
	}
	if query.Has("days") {
		days, err := strconv.Atoi(strings.TrimSpace(query.Get("days")))
		if err != nil || days < 0 || days > 365000 {
			return opts, fmt.Errorf("days must be between 0 and 365000")
		}
		if days > 0 {
			opts.days = days
		}
	}
	if query.Has("page") || query.Has("page_size") {
		page, size, err := common.ParsePagination(query)
		if err != nil {
			return opts, err
		}
		opts.offset, opts.limit = (page-1)*size, size
	}
	return opts, nil
}

func (s *Service) listUserActivityReport(ctx context.Context, opts userActivityReportOptions) ([]map[string]interface{}, int64, error) {
	cutoff := time.Now().AddDate(0, 0, -opts.days)
	if opts.inactive {
		counts, err := s.userDAO.ListInactiveByPlan(ctx, dao.DB, opts.plan, cutoff)
		if err != nil {
			return nil, 0, fmt.Errorf("query inactive users: %w", err)
		}
		rows := make([]map[string]interface{}, 0, len(counts)+1)
		var inactive int64
		for _, count := range counts {
			inactive += count.InactiveCount
			rows = append(rows, map[string]interface{}{"plan": count.PlanName, "inactive_users": common.FormatNumber(count.InactiveCount)})
		}
		rows = append(rows, map[string]interface{}{"plan": "Total", "inactive_users": common.FormatNumber(inactive)})
		total := int64(len(rows))
		start, end := min(opts.offset, len(rows)), len(rows)
		if opts.limit > 0 && opts.limit < end-start {
			end = start + opts.limit
		}
		return rows[start:end], total, nil
	}
	users, total, err := s.userDAO.ListPlanRows(ctx, dao.DB, opts.plan, cutoff, opts.offset, opts.limit)
	if err != nil {
		return nil, 0, fmt.Errorf("query users by plan: %w", err)
	}
	rows := make([]map[string]interface{}, 0, len(users))
	for i, user := range users {
		rows = append(rows, map[string]interface{}{
			"rank": opts.offset + i + 1, "user": user.Email, "plan": user.PlanName,
			"last_login": user.LastLoginTime.Format("2006-01-02 15:04:05"),
		})
	}
	return rows, total, nil
}

#!/usr/bin/env python3
# Copyright(C) 2026 InfiniFlow, Inc. All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      https://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""
Convert Elasticsearch index mapping to Infinity table schema and indexes.

Usage:
    python es_to_infinity.py <es_mapping.json> [--output output.sql] [--execute]

Example:
    python es_to_infinity.py /path/to/es_mapping.json --output schema.sql
    python es_to_infinity.py /path/to/es_mapping.json --execute  # Execute directly on Infinity
"""

import json
import argparse
import re
from typing import Dict, List, Any, Optional, Tuple
from dataclasses import dataclass, field


@dataclass
class ColumnDef:
    """Infinity column definition"""

    name: str
    data_type: str
    nullable: bool = True
    comment: str = ""

    def to_sql(self) -> str:
        # Handle vector type format: "vector,1024,float" -> "vector(float, 1024)"
        if self.data_type.startswith("vector,"):
            parts = self.data_type.split(",")
            if len(parts) == 3:
                return f"{self.name} vector({parts[2]}, {parts[1]})"
        return f"{self.name} {self.data_type}"


@dataclass
class IndexDef:
    """Infinity index definition"""

    index_name: str
    columns: List[str]
    index_type: str  # HNSW, IVF, FULLTEXT, SECONDARY
    params: Dict[str, Any] = field(default_factory=dict)

    def to_sql(self) -> str:
        params_str = ""
        if self.params:
            params_list = [f'"{k}"="{v}"' for k, v in self.params.items()]
            params_str = f" WITH ({', '.join(params_list)})"

        if self.index_type == "HNSW":
            return f'CREATE INDEX {self.index_name} ON TABLE ("{self.columns[0]}") USING HNSW{params_str};'
        elif self.index_type == "FULLTEXT":
            return f'CREATE INDEX {self.index_name} ON TABLE ("{self.columns[0]}") USING FULLTEXT{params_str};'
        elif self.index_type == "SECONDARY":
            return f'CREATE INDEX {self.index_name} ON TABLE ("{self.columns[0]}") USING SECONDARY;'
        else:
            return f'CREATE INDEX {self.index_name} ON TABLE ("{self.columns[0]}") USING {self.index_type}{params_str};'


class ESToInfinityConverter:
    """Convert ES mapping to Infinity schema

    Following RAGFlow's approach (rag/app/table.py):
    - Fixed schema fields -> individual columns
    - Dynamic/table fields -> stored in chunk_data JSON column
    """

    # ES type -> Infinity type mapping
    TYPE_MAPPING = {
        "integer": "integer",
        "int": "integer",
        "long": "bigint",
        "unsigned_long": "bigint",
        "short": "smallint",
        "byte": "tinyint",
        "float": "float",
        "double": "double",
        "half_float": "float16",
        "scaled_float": "double",
        "boolean": "boolean",
        "text": "varchar",
        "keyword": "varchar",
        "date": "datetime",
        "date_nanos": "datetime",
        "datetime": "datetime",
        "timestamp": "timestamp",
        # Vectors will be handled specially
        "dense_vector": "vector",
        "sparse_vector": "sparse",
        # Not directly supported, map to varchar
        "geo_point": "varchar",
        "geo_shape": "varchar",
        "binary": "varchar",
        "ip": "varchar",
        "completion": "varchar",
        # Complex types - supported via JSON type
        "nested": "json",  # Store as JSON
        "object": "json",  # Store as JSON
        # Rank features - stored as varchar with special analyzer
        "rank_feature": "varchar",  # Store as varchar, will create special index
        "rank_features": "varchar",  # Store as varchar, will create special index
    }

    # Fixed schema fields from infinity_mapping.json (ragflow_enterprise)
    # These fields are pre-defined in the schema and should be individual columns
    FIXED_SCHEMA_FIELDS = {
        "id",
        "doc_id",
        "kb_id",
        "mom_id",
        "create_time",
        "create_timestamp_flt",
        "img_id",
        "docnm",
        "name_kwd",
        "tag_kwd",
        "important_kwd_empty_count",
        "important_keywords",
        "questions",
        "content",
        "authors",
        "page_num_int",
        "top_int",
        "position_int",
        "weight_int",
        "weight_flt",
        "rank_int",
        "rank_flt",
        "available_int",
        "knowledge_graph_kwd",
        "entities_kwd",
        "pagerank_fea",
        "tag_feas",
        "from_entity_kwd",
        "to_entity_kwd",
        "entity_kwd",
        "entity_type_kwd",
        "source_id",
        "n_hop_with_weight",
        "mom_with_weight",
        "removed_kwd",
        "doc_type_kwd",
        "toc_kwd",
        "raptor_kwd",
        # ES field aliases (mapped to fixed schema fields in RAGFlow)
        "docnm_kwd",
        "title_tks",
        "title_sm_tks",
        "important_kwd",
        "important_tks",
        "question_kwd",
        "question_tks",
        "content_with_weight",
        "content_ltks",
        "content_sm_ltks",
        "authors_tks",
        "authors_sm_tks",
        # Vector fields
        "q_512_vec",
        "q_768_vec",
        "q_1024_vec",
        "q_1536_vec",
    }

    def __init__(self):
        self.columns: Dict[str, List[ColumnDef]] = {}
        self.indexes: Dict[str, List[IndexDef]] = {}
        self.warnings: List[str] = []
        self.json_fields: Dict[str, List[str]] = {}  # index_name -> list of fields for chunk_data

    def convert_es_type_to_infinity(self, es_type: str, field_name: str, properties: Dict[str, Any]) -> Optional[Tuple[str, str]]:
        """
        Convert ES field type to Infinity type.
        Returns (infinity_type, comment) or None if not supported.
        """
        if es_type == "dense_vector":
            dims = properties.get("dims", 128)
            element_type = "float"  # ES default
            # Check index_options for element type hints
            return (f"vector,{dims},{element_type}", f"ES dense_vector with {dims} dims")

        elif es_type == "sparse_vector":
            return ("sparse,float", "ES sparse_vector")

        elif es_type == "text":
            analyzer = properties.get("analyzer", "standard")
            return ("varchar", f"ES text with analyzer: {analyzer}")

        elif es_type == "keyword":
            return ("varchar", "ES keyword")

        elif es_type == "date":
            format_str = properties.get("format", "")
            return ("datetime", f"ES date format: {format_str}" if format_str else "ES date")

        elif es_type in ("geo_point", "geo_shape", "binary", "ip"):
            return ("varchar", f"ES {es_type} (stored as varchar)")

        elif es_type == "nested":
            # Store nested objects as JSON
            return ("json", "ES nested (stored as JSON)")

        elif es_type == "object":
            # Store object type as JSON
            return ("json", "ES object (stored as JSON)")

        elif es_type == "rank_feature":
            # Store rank_feature as varchar, will use special analyzer for indexing
            return ("varchar", "ES rank_feature (stored as varchar, indexed with rankfeatures analyzer)")

        elif es_type == "rank_features":
            # Store rank_features as varchar, will use special analyzer for indexing
            return ("varchar", "ES rank_features (stored as varchar, indexed with rankfeatures analyzer)")

        elif es_type in self.TYPE_MAPPING:
            infinity_type = self.TYPE_MAPPING[es_type]
            if infinity_type is None:
                return None
            return (infinity_type, f"ES {es_type}")

        else:
            # Unknown type, default to varchar
            return ("varchar", f"Unknown ES type: {es_type}")

    def convert_index(self, field_name: str, properties: Dict[str, Any]) -> Optional[IndexDef]:
        """Convert ES index to Infinity index"""
        es_type = properties.get("type", "")

        # Vector index
        if es_type == "dense_vector":
            if properties.get("index", False):
                similarity = properties.get("similarity", "cosine")
                index_options = properties.get("index_options", {})

                # HNSW parameters
                m = index_options.get("m", 16)
                ef_construction = index_options.get("ef_construction", 200)

                # Map similarity metric
                metric_map = {"cosine": "cosine", "l2_norm": "l2", "dot_product": "ip", "ip": "ip"}
                metric = metric_map.get(similarity, "cosine")

                return IndexDef(index_name=f"idx_{field_name}", columns=[field_name], index_type="HNSW", params={"M": str(m), "ef_construction": str(ef_construction), "metric": metric})

        # Fulltext index for text fields with analyzer
        elif es_type == "text" and properties.get("analyzer"):
            if properties.get("index", True):
                return IndexDef(index_name=f"idx_{field_name}", columns=[field_name], index_type="FULLTEXT", params={})

        # Secondary index for keyword/other indexed fields
        elif es_type in ("keyword", "integer", "long", "float", "double", "date"):
            if properties.get("index", True):
                return IndexDef(index_name=f"idx_{field_name}", columns=[field_name], index_type="SECONDARY", params={})

        # Rank features index - use FULLTEXT with rankfeatures analyzer
        elif es_type in ("rank_feature", "rank_features"):
            if properties.get("index", True):
                return IndexDef(index_name=f"idx_{field_name}", columns=[field_name], index_type="FULLTEXT", params={"analyzer": "rankfeatures"})

        return None

    def process_properties(self, properties: Dict[str, Any], prefix: str = "", index_name: str = "default") -> Tuple[List[ColumnDef], List[IndexDef], List[str]]:
        """Recursively process ES properties.

        Follows RAGFlow's approach (rag/app/table.py):
        - Fixed schema fields -> separate columns
        - Dynamic/table fields -> stored in chunk_data JSON column

        Returns:
            Tuple[List[ColumnDef], List[IndexDef], List[str]]: columns, indexes, json_field_names
        """
        columns: List[ColumnDef] = []
        indexes: List[IndexDef] = []
        json_fields: List[str] = []  # Fields to store in chunk_data JSON

        for field_name, field_props in properties.items():
            # Build the full field name with prefix
            raw_full_name = f"{prefix}{field_name}" if prefix else field_name
            # Replace non-alphanumeric characters with underscores
            full_name = re.sub(r"[^a-zA-Z0-9]", "_", raw_full_name)

            es_type = field_props.get("type", "")

            # No explicit type, might be a nested object
            if not es_type:
                if "properties" in field_props:
                    # Recursively process nested object
                    nested_cols, nested_idxs, nested_json = self.process_properties(field_props["properties"], f"{full_name}_", index_name)
                    columns.extend(nested_cols)
                    indexes.extend(nested_idxs)
                    json_fields.extend(nested_json)
                continue

            # Check if this is a fixed schema field
            # Reference RAGFlow: fixed fields are defined in infinity_mapping.json
            # Dynamic fields (e.g., column names starting with numbers in tables) go to chunk_data JSON
            is_fixed_field = full_name in self.FIXED_SCHEMA_FIELDS

            # Check if this is a vector field (q_*_vec pattern)
            is_vector_field = re.match(r"^q_\d+_vec$", full_name) is not None

            # Field names starting with numbers or non-fixed fields -> store in chunk_data JSON
            if not is_fixed_field and not is_vector_field:
                # Store in chunk_data JSON column, keeping original field name
                json_fields.append(raw_full_name)
                # Clean field name for warning (remove newlines/control chars)
                safe_name = "".join(c if c.isprintable() and c not in "\r\n" else "?" for c in raw_full_name)
                self.warnings.append(f"Field '{safe_name}' will be stored in chunk_data JSON column (not a fixed schema field)")
                continue

            result = self.convert_es_type_to_infinity(es_type, full_name, field_props)
            if result is None:
                # Clean field name for warning
                safe_name = "".join(c if c.isprintable() and c not in "\r\n" else "?" for c in full_name)
                self.warnings.append(f"Skipping field '{safe_name}' with unsupported type '{es_type}'")
                continue

            infinity_type, comment = result

            col = ColumnDef(
                name=full_name,
                data_type=infinity_type,
                nullable=True,
                comment=comment,
            )
            columns.append(col)

            if field_props.get("index", True) and es_type not in ("nested", "object"):
                idx = self.convert_index(full_name, field_props)
                if idx:
                    indexes.append(idx)

        return columns, indexes, json_fields

    def convert_index_mapping(self, index_name: str, mapping: Dict[str, Any]) -> Tuple[List[ColumnDef], List[IndexDef]]:
        """Convert a single ES index mapping"""
        mappings = mapping.get("mappings", {})
        properties = mappings.get("properties", {})

        columns, indexes, json_fields = self.process_properties(properties, "", index_name)

        # Store json_fields for later use
        self.json_fields[index_name] = json_fields

        # Add chunk_data JSON column if there are dynamic fields
        # Reference: rag/app/table.py line 276-279
        if json_fields:
            chunk_data_col = ColumnDef(name="chunk_data", data_type="json", nullable=True, comment="Dynamic fields stored as JSON (for table parser, etc.)")
            columns.append(chunk_data_col)

        return columns, indexes

    def convert_file(self, input_file: str) -> Dict[str, Tuple[List[ColumnDef], List[IndexDef]]]:
        """Convert ES mapping file to Infinity schema"""
        with open(input_file, "r", encoding="utf-8") as f:
            content = f.read()

        # Remove curl progress lines (lines starting with numbers like "100 48981...")
        lines = content.split("\n")
        cleaned_lines = []
        for line in lines:
            # Skip lines that look like curl progress output
            stripped = line.strip()
            if stripped and stripped[0].isdigit():
                # Check if this looks like curl progress (contains multiple numbers and --:--:--)
                if re.match(r"^\d+\s+\d+", stripped) and "--:--:--" in stripped:
                    # This is a curl progress line, but it may have JSON content after it
                    # Try to extract the JSON part after the progress info
                    parts = stripped.split("  ")
                    for part in reversed(parts):
                        if part.strip() and not part.strip()[0].isdigit():
                            cleaned_lines.append("  " + part.strip())
                            break
                    continue
            cleaned_lines.append(line)

        cleaned_content = "\n".join(cleaned_lines)

        try:
            data = json.loads(cleaned_content)
        except json.JSONDecodeError as e:
            print(f"Error parsing JSON: {e}")
            print("Trying alternative cleanup method...")
            # Alternative: extract JSON object using regex
            match = re.search(r"\{[\s\S]*\}", cleaned_content)
            if match:
                data = json.loads(match.group())
            else:
                raise

        result = {}

        # Handle different formats
        if "mappings" in data:
            # Single index format
            columns, indexes = self.convert_index_mapping("default", data)
            result["default"] = (columns, indexes)
        else:
            # Multiple indices format
            for index_name, index_data in data.items():
                if isinstance(index_data, dict) and "mappings" in index_data:
                    columns, indexes = self.convert_index_mapping(index_name, index_data)
                    result[index_name] = (columns, indexes)

        return result

    def generate_sql(self, index_name: str, columns: List[ColumnDef], indexes: List[IndexDef]) -> str:
        """Generate SQL statements for table and indexes"""
        lines = []

        # Sanitize table name (ES index names can have special chars)
        table_name = re.sub(r"[^a-zA-Z0-9_]", "_", index_name)
        if table_name[0].isdigit():
            table_name = f"t_{table_name}"

        lines.append(f"-- Table: {table_name} (from ES index: {index_name})")

        # Add comment about JSON fields if any
        json_fields = self.json_fields.get(index_name, [])
        if json_fields:
            lines.append(f"-- Dynamic fields stored in chunk_data JSON column ({len(json_fields)} fields):")
            for jf in json_fields[:10]:
                lines.append(f"--   - {jf}")
            if len(json_fields) > 10:
                lines.append(f"--   ... and {len(json_fields) - 10} more fields")

        lines.append(f"DROP TABLE IF EXISTS {table_name};")

        # Generate CREATE TABLE
        col_defs = [col.to_sql() for col in columns]
        lines.append(f"CREATE TABLE {table_name} (")
        lines.append(",\n".join(f"    {c}" for c in col_defs))
        lines.append(");")
        lines.append("")

        # Generate CREATE INDEX statements
        for idx in indexes:
            idx_sql = idx.to_sql().replace("ON TABLE", f"ON {table_name}")
            lines.append(idx_sql)

        # Add example query for JSON fields
        if json_fields:
            lines.append("")
            lines.append("-- Example query for JSON fields in chunk_data:")
            lines.append(f"-- SELECT doc_id, json_extract_string(chunk_data, '$.field_name') FROM {table_name};")

        lines.append("")
        return "\n".join(lines)

    def generate_python_code(self, index_name: str, columns: List[ColumnDef], indexes: List[IndexDef]) -> str:
        """Generate Python code to create table and indexes"""
        lines = []
        lines.append(f'"""Create table and indexes for ES index: {index_name}"""')
        lines.append("")
        lines.append("import infinity")
        lines.append("from infinity.common import ConflictType")
        lines.append("")
        lines.append("# Connect to Infinity")
        lines.append('infinity_instance = infinity.connect(infinity.common.NetworkAddress("127.0.0.1", 23817))')
        lines.append('db_instance = infinity_instance.get_database("default_db")')
        lines.append("")

        # Generate table code
        table_code = self.generate_python_table_code(index_name, columns, indexes)
        lines.append(table_code)

        lines.append("")
        lines.append("infinity_instance.disconnect()")

        return "\n".join(lines)

    def generate_python_table_code(self, index_name: str, columns: List[ColumnDef], indexes: List[IndexDef]) -> str:
        """Generate Python code for a single table (without import/connect/disconnect)"""
        lines = []

        # Sanitize table name
        table_name = re.sub(r"[^a-zA-Z0-9_]", "_", index_name)
        if table_name[0].isdigit():
            table_name = f"t_{table_name}"

        lines.append(f"# Table: {index_name}")

        # Add comment about JSON fields
        json_fields = self.json_fields.get(index_name, [])
        if json_fields:
            lines.append(f"# Dynamic fields stored in chunk_data JSON column: {len(json_fields)} fields")
            lines.append("# These fields should be stored in chunk_data when inserting data")
            lines.append("")

        lines.append("# Drop existing table")
        lines.append(f'db_instance.drop_table("{table_name}", ConflictType.Ignore)')
        lines.append("")

        # Generate table schema
        lines.append("# Create table")
        lines.append(f'table_instance = db_instance.create_table("{table_name}", {{')
        for col in columns:
            lines.append(f'    "{col.name}": {{"type": "{col.data_type}"}},')
        lines.append("})")
        lines.append("")

        # Generate indexes
        for idx in indexes:
            params_str = ""
            if idx.params:
                params_list = [f'"{k}": {repr(v)}' for k, v in idx.params.items()]
                params_str = f", {{{', '.join(params_list)}}}"

            if idx.index_type == "HNSW":
                lines.append(f"# Create HNSW index on {idx.columns[0]}")
                lines.append(f'table_instance.create_index("{idx.index_name}",')
                lines.append(f'    infinity.index.IndexInfo("{idx.columns[0]}",')
                lines.append(f"        infinity.index.IndexType.Hnsw{params_str}),")
                lines.append("    ConflictType.Error)")
            elif idx.index_type == "FULLTEXT":
                lines.append(f"# Create fulltext index on {idx.columns[0]}")
                if idx.params.get("analyzer") == "rankfeatures":
                    lines.append("    # Note: Using rankfeatures analyzer for rank_feature/rank_features type")
                lines.append(f'table_instance.create_index("{idx.index_name}",')
                lines.append(f'    infinity.index.IndexInfo("{idx.columns[0]}",')
                lines.append(f"        infinity.index.IndexType.FullText{params_str}),")
                lines.append("    ConflictType.Error)")
            elif idx.index_type == "SECONDARY":
                lines.append(f"# Create secondary index on {idx.columns[0]}")
                lines.append(f'table_instance.create_index("{idx.index_name}",')
                lines.append(f'    infinity.index.IndexInfo("{idx.columns[0]}",')
                lines.append("        infinity.index.IndexType.Secondary),")
                lines.append("    ConflictType.Error)")

        lines.append("")
        lines.append(f'print("Table {table_name} created successfully")')

        return "\n".join(lines)


def main():
    parser = argparse.ArgumentParser(description="Convert Elasticsearch index mapping to Infinity schema")
    parser.add_argument("input_file", help="Path to ES mapping JSON file")
    parser.add_argument("--output", "-o", help="Output file path (default: print to stdout)")
    parser.add_argument("--format", "-f", choices=["sql", "python"], default="sql", help="Output format (default: sql)")
    parser.add_argument("--execute", "-e", action="store_true", help="Execute directly on Infinity server (requires Infinity running)")
    parser.add_argument("--index", help="Process only the specified index name")

    args = parser.parse_args()

    converter = ESToInfinityConverter()

    print(f"Processing ES mapping file: {args.input_file}")
    results = converter.convert_file(args.input_file)

    if not results:
        print("No valid index mappings found in the input file.")
        return

    output_lines = []
    comment_prefix = "--" if args.format == "sql" else "#"
    output_lines.append(f"{comment_prefix} Auto-generated Infinity schema from Elasticsearch mapping")
    output_lines.append(f"{comment_prefix} Source: {args.input_file}")
    output_lines.append("")

    # Filter tables if --index is specified
    filtered_results = {}
    for index_name, (columns, indexes) in results.items():
        if args.index and index_name != args.index:
            continue
        filtered_results[index_name] = (columns, indexes)
        print(f"\nProcessing index: {index_name}")
        print(f"  Columns: {len(columns)}")
        print(f"  Indexes: {len(indexes)}")

    if args.format == "sql":
        for index_name, (columns, indexes) in filtered_results.items():
            output_lines.append(converter.generate_sql(index_name, columns, indexes))
    else:
        # Python format
        if len(filtered_results) == 1:
            # Single table: use complete code with import/connect/disconnect
            for index_name, (columns, indexes) in filtered_results.items():
                output_lines.append(converter.generate_python_code(index_name, columns, indexes))
        else:
            # Multiple tables: share import/connect/disconnect
            output_lines.append('"""Create tables and indexes from ES mappings"""')
            output_lines.append("")
            output_lines.append("import infinity")
            output_lines.append("from infinity.common import ConflictType")
            output_lines.append("")
            output_lines.append("# Connect to Infinity")
            output_lines.append('infinity_instance = infinity.connect(infinity.common.NetworkAddress("127.0.0.1", 23817))')
            output_lines.append('db_instance = infinity_instance.get_database("default_db")')
            output_lines.append("")

            for index_name, (columns, indexes) in filtered_results.items():
                output_lines.append(converter.generate_python_table_code(index_name, columns, indexes))
                output_lines.append("")

            output_lines.append("infinity_instance.disconnect()")
            output_lines.append('print("All tables created successfully")')

    # Print warnings
    if converter.warnings:
        output_lines.append("")
        output_lines.append(f"{comment_prefix} WARNINGS:")
        for warning in converter.warnings:
            # Remove/replace characters that could break comments
            clean_warning = warning.replace("\n", " ").replace("\r", "")
            if args.format == "python":
                # For Python, also handle quotes in a safe way
                clean_warning = clean_warning.replace("'", "'")
            output_lines.append(f"{comment_prefix}   {clean_warning}")

    output = "\n".join(output_lines)

    if args.output:
        with open(args.output, "w", encoding="utf-8") as f:
            f.write(output)
        print(f"\nOutput written to: {args.output}")
    else:
        print("\n" + "=" * 60)
        print("GENERATED SCHEMA:")
        print("=" * 60)
        print(output)

    if args.execute:
        print("\nExecuting on Infinity server...")
        # TODO: Implement direct execution
        print("Direct execution not implemented yet. Please use --output to save and execute manually.")


if __name__ == "__main__":
    main()

#
#  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
#
#  Licensed under the Apache License, Version 2.0 (the "License");
#  you may not use this file except in compliance with the License.
#  You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
#  Unless required by applicable law or agreed to in writing, software
#  distributed under the License is distributed on an "AS IS" BASIS,
#  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#  See the License for the specific language governing permissions and
#  limitations under the License.
#
"""
MySQL Data Check and Fix Script

This script provides utilities to check and fix data integrity issues
in MySQL database, particularly for user password fields.

Usage:
    python check_and_fix_mysql_data.py -c /path/to/service_conf.yaml --check-password
    python check_and_fix_mysql_data.py -c /path/to/service_conf.yaml --check-password --fix
K8s MySQL Access (from outside the cluster):
    1. Get MySQL credentials from k8s secrets:
        For root password:
            kubectl get secret -n mysql-password -o jsonpath='{.data.password}' | base64 -d
        For non-root user (check StatefulSet env vars):
            kubectl get statefulset -n mysql -o json | grep -A 10 'env:'

    2. Create service_conf.yaml with the obtained credentials:
        mysql:
        name: rag_flow
        user: <user_from_env>
        password:
        host: 127.0.0.1 # Use localhost when using port-forward
        port: 3306

    3. Set up port-forward to MySQL service:
        kubectl port-forward -n svc/mysql 3306:3306 &

    4. Run the script:
        python check_and_fix_mysql_data.py -c service_conf.yaml --check-password

    5. Cleanup port-forward when done:
        pkill -f "kubectl port-forward.*mysql"

"""

import argparse
import base64
import logging
import sys
from pathlib import Path

import peewee
from ruamel.yaml import YAML
from werkzeug.security import generate_password_hash

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s - %(levelname)s - %(message)s",
)
logger = logging.getLogger(__name__)


def load_mysql_config(config_path: str) -> dict:
    """Load MySQL configuration from YAML config file.

    Args:
        config_path: Path to the YAML configuration file.

    Returns:
        Dictionary containing MySQL connection parameters.
    """
    config_file = Path(config_path)
    if not config_file.exists():
        raise FileNotFoundError(f"Config file not found: {config_path}")

    yaml = YAML(typ="safe", pure=True)
    with open(config_file) as f:
        config = yaml.load(f)

    mysql_config = config.get("mysql", {})
    if not mysql_config:
        raise ValueError("MySQL configuration not found in config file")

    return {
        "database": mysql_config.get("name", "rag_flow"),
        "user": mysql_config.get("user", "root"),
        "password": mysql_config.get("password", ""),
        "host": mysql_config.get("host", "localhost"),
        "port": mysql_config.get("port", 3306),
    }


def create_db_connection(mysql_config: dict) -> peewee.MySQLDatabase:
    """Create a MySQL database connection using peewee.

    Args:
        mysql_config: Dictionary containing MySQL connection parameters.

    Returns:
        peewee.MySQLDatabase instance.
    """
    db = peewee.MySQLDatabase(
        mysql_config["database"],
        user=mysql_config["user"],
        password=mysql_config["password"],
        host=mysql_config["host"],
        port=mysql_config["port"],
        charset="utf8mb4",
    )
    return db


def check_user_passwords(db: peewee.MySQLDatabase, fix: bool = False) -> int:
    """Check user table for unhashed passwords.

    This function scans the user table for records where the password field
    is non-empty but not hashed by werkzeug's generate_password_hash.
    Project uses 'scrypt:' as the fixed hash prefix.

    Args:
        db: peewee.MySQLDatabase connection instance.
        fix: If True, fix the unhashed passwords by applying base64 encoding
             followed by generate_password_hash.

    Returns:
        Number of records with unhashed passwords.
    """
    cursor = db.execute_sql("SELECT id, email, password FROM user WHERE password IS NOT NULL AND password != '' AND password NOT LIKE 'scrypt:%%'")
    rows = cursor.fetchall()

    unhashed_records = []
    for row in rows:
        user_id, email, password = row
        unhashed_records.append(
            {
                "id": user_id,
                "email": email,
                "password": password,
            }
        )

    if not unhashed_records:
        logger.info("✅ All user passwords are properly hashed")
        return 0

    logger.warning(f"⚠️ Found {len(unhashed_records)} user(s) with unhashed passwords:")

    for record in unhashed_records:
        logger.warning(f"   - ID: {record['id']}, Email: {record['email']}")

    fixed_count = 0
    if fix:
        logger.info("🔧 Fixing unhashed passwords...")
        for record in unhashed_records:
            # First encode with base64, then hash with werkzeug
            encoded_password = base64.b64encode(record["password"].encode("utf-8")).decode("utf-8")
            hashed_password = generate_password_hash(encoded_password)

            cursor = db.execute_sql(
                "UPDATE user SET password = %s WHERE id = %s",
                (hashed_password, record["id"]),
            )
            fixed_count += cursor.rowcount
            logger.info(f"   ✅ Fixed password for user: {record['email']}")
        logger.info(f"✅ Fixed {fixed_count} password(s)")
    else:
        logger.info("💡 Run with --fix to apply fixes")

    return fixed_count if fix else len(unhashed_records)


def main():
    """Main entry point for the script."""
    parser = argparse.ArgumentParser(
        description="Check and fix MySQL data integrity issues",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  # Check user passwords only
  python check_and_fix_mysql_data.py -c /path/to/service_conf.yaml --check-password

  # Check and fix user passwords
  python check_and_fix_mysql_data.py -c /path/to/service_conf.yaml --check-password --fix

  # Run all checks
  python check_and_fix_mysql_data.py -c /path/to/service_conf.yaml --all

  # Run all checks and apply fixes
  python check_and_fix_mysql_data.py -c /path/to/service_conf.yaml --all --fix
        """,
    )

    parser.add_argument(
        "-c",
        "--config",
        type=str,
        required=True,
        help="Path to the YAML configuration file (e.g., conf/service_conf.yaml)",
    )

    parser.add_argument(
        "--check-password",
        action="store_true",
        help="Check user table password field for unhashed values",
    )

    parser.add_argument(
        "--all",
        action="store_true",
        help="Run all available checks",
    )

    parser.add_argument(
        "--fix",
        action="store_true",
        help="Apply fixes for detected issues (default: check only)",
    )

    args = parser.parse_args()

    # If no specific check is selected, show help
    if not args.check_password and not args.all:
        parser.print_help()
        sys.exit(1)

    # Load MySQL configuration
    try:
        mysql_config = load_mysql_config(args.config)
        logger.info(f"📁 Loaded MySQL config from: {args.config}")
        logger.info(f"   Host: {mysql_config['host']}:{mysql_config['port']}")
        logger.info(f"   Database: {mysql_config['database']}")
    except Exception as e:
        logger.error(f"❌ Failed to load config: {e}")
        sys.exit(1)

    # Define check stages: (flag_name, stage_name, check_function)
    check_stages = [
        ("check_password", "User Password Check", check_user_passwords),
    ]

    # Determine which stages to run
    stages_to_run = []
    for flag_name, stage_name, check_func in check_stages:
        if getattr(args, flag_name) or args.all:
            stages_to_run.append((stage_name, check_func))

    total_stages = len(stages_to_run)

    # Connect to database
    db = None
    try:
        db = create_db_connection(mysql_config)
        db.connect()
        logger.info("✅ Connected to MySQL database")

        total_issues = 0

        # Run checks with stage progress
        for current_stage, (stage_name, check_func) in enumerate(stages_to_run, start=1):
            logger.info(f"[Stage {current_stage}/{total_stages}] 🔍 {stage_name}...")
            issues = check_func(db, fix=args.fix)
            total_issues += issues

        # Summary
        logger.info("=" * 50)
        if total_issues > 0:
            if args.fix:
                logger.info(f"✅ Fixed {total_issues} issue(s)")
            else:
                logger.info(f"⚠️ Found {total_issues} issue(s) (run with --fix to apply fixes)")
        else:
            logger.info("✅ No issues found")

    except Exception as e:
        logger.error(f"❌ Runtime error: {e}")
        sys.exit(1)
    finally:
        if db is not None:
            db.close()
            logger.info("🔌 Database connection closed")


if __name__ == "__main__":
    main()

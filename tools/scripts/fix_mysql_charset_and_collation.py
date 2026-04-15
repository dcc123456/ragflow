import os

import pymysql


host = os.getenv("MYSQL_HOST", "mysql")
user = os.getenv("MYSQL_USER", "root")
password = os.getenv("MYSQL_PASSWORD", "infini_rag_flow")
database = os.getenv("MYSQL_DATABASE") or os.getenv("MYSQL_DB_NAME", "rag_flow")


if __name__ == "__main__":
    try:
        conn = pymysql.connect(host=host, user=user, password=password, database=database)
        with conn.cursor() as cursor:
            cursor.execute(
                """
                SELECT TABLE_NAME, TABLE_COLLATION
                FROM information_schema.TABLES
                WHERE TABLE_SCHEMA = %s
                  AND TABLE_TYPE = 'BASE TABLE'
                  AND TABLE_COLLATION != 'utf8mb4_general_ci'
                """,
                (database,),
            )

            for table_name, current_collation in cursor.fetchall():
                table_name = table_name.strip()
                print(
                    f"Fixing: {table_name}, current_collation: {current_collation}, "
                    f"target_collation: utf8mb4_general_ci"
                )
                try:
                    sql = (
                        f"ALTER TABLE {table_name} CONVERT TO CHARACTER SET utf8mb4 "
                        "COLLATE utf8mb4_general_ci"
                    )
                    cursor.execute(sql)
                    conn.commit()
                    print("  ✅ Done\n")
                except Exception as e:
                    print(f"  ❌ Error: {e}\n")
        conn.close()
    except Exception as e:
        print(f"error: {e}")

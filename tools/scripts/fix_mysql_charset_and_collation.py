import pymysql

conn = pymysql.connect(
    host='mysql',
    user='ragflow',
    password='infini_rag_flow',
    database='rag_flow'
)

if __name__ == '__main__':
    cursor = conn.cursor()

    cursor.execute('''
        SELECT TABLE_NAME, TABLE_COLLATION
        FROM information_schema.TABLES 
        WHERE TABLE_SCHEMA = 'rag_flow' 
        AND TABLE_TYPE = 'BASE TABLE'
        AND TABLE_COLLATION != 'utf8mb4_general_ci'
    ''')

    for row in cursor.fetchall():
        table_name = row[0]
        # remove white space
        table_name = table_name.strip()
        current_collation = row[1]

        print(f'Fixing: {table_name},table name: {table_name}')
        try:
            sql = f'ALTER TABLE {table_name} CONVERT TO CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci'
            cursor.execute(sql)
            conn.commit()
            print('  ✅ Done\n')
        except Exception as e:
            print(f'  ❌ Error: {e}\n')

    conn.close()
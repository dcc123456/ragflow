import pymysql
import os

host = os.getenv('MYSQL_HOST', 'mysql')
user = os.getenv('MYSQL_USER', 'root')
password = os.getenv('MYSQL_PASSWORD', 'infini_rag_flow')
database = os.getenv('MYSQL_DATABASE', 'rag_flow')

if __name__ == '__main__':

    try:
        conn = pymysql.connect(host=host, user=user, password=password, database=database)
        with conn.cursor() as cursor:
            cursor.execute(
                'SELECT DEFAULT_CHARACTER_SET_NAME, DEFAULT_COLLATION_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = %s',
                (database,))
            result = cursor.fetchone()
            print(f'database DEFAULT_CHARACTER_SET_NAME: {result[0]}, DEFAULT_COLLATION_NAME: {result[1]}')

            cursor.execute('SELECT TABLE_NAME, TABLE_COLLATION FROM information_schema.TABLES WHERE TABLE_SCHEMA = %s',
                       (database,))
            collation_map = {}
            for table_name, collation in cursor.fetchall():
                if collation_map.get(collation):
                    collation_map[collation].append(table_name)
                else:
                    collation_map[collation] = [table_name]
            for collation, table_names in collation_map.items():
                print(f'collation: {collation}, table_names: {table_names}')
        conn.close()
    except Exception as e:
        print(f'error: {e}')
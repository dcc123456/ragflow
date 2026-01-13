import time
import random
from concurrent.futures import ThreadPoolExecutor

import requests
from rag.utils.redis_conn import REDIS_CONN

tokens = [
    "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",#kevinhu
]


def get_user_stared(uname):
    global tokens
    url = f"https://api.github.com/users/{uname}/starred?per_page=100"
    headers = {
        "Authorization": "Bearer %s"%random.choice(tokens),
        "Accept": "application/vnd.github.v3+json"
    }
    try:
        response = requests.get(url, headers=headers)
        response.raise_for_status()
        return "infiniflow/ragflow" in set([s["full_name"] for s in response.json()])
    except Exception as e:
        print(f"Get stared project fail: {e}")
    return False


def get_repo_stars():
    global tokens
    # GitHub API URL
    url = "https://api.github.com/repos/infiniflow/ragflow"
    while True:
        headers = {
            "Authorization": "Bearer %s"%random.choice(tokens),
            "Accept": "application/vnd.github.v3+json"
        }
        try:
            response = requests.get(url, headers=headers)
            response.raise_for_status()
            print("TOTAL: ", response.json()['stargazers_count'])
            return response.json()['stargazers_count']
        except Exception as e:
            print(f"Get stars count fail: {e}")
            time.sleep(30)
    return 0


def update_stars(from_page, to_page, desc=True, page_size=300):
    global tokens
    base_url = "https://api.github.com/repos/infiniflow/ragflow/stargazers"

    page_num = 0
    page = to_page
    while True:
        headers = {
            "Authorization": "Bearer %s"%random.choice(tokens),
            "Accept": "application/vnd.github.v3+json"
        }
        try:
            url = f"{base_url}?page={page}&per_page={page_size}"
            response = requests.get(url,headers=headers)
            response.raise_for_status()
            stargazers = response.json()
            if not stargazers and not desc:
                break
            for item in stargazers:
                REDIS_CONN.set(item["login"], 1, exp=36000)
        except Exception as e:
            print(f"Update stars fail. Failed to retrieve star user information: {e}")
            time.sleep(30)
        page_num += 1
        if desc:
            page -= 1
            if page < from_page:
                break
        else:
            page += 1
            if page > from_page:
                break
    return page_num


def update_all_the_time():
    while True:
        ps = random.randint(100, 500)
        page_num = int(get_repo_stars()/ps + 0.5)
        update_stars(page_num, 1, False, ps)


if __name__ == "__main__":
    exe = ThreadPoolExecutor(max_workers=2)
    exe.submit(update_all_the_time)
    while True:
        ps = random.randint(100, 500)
        page_num = int(get_repo_stars()/ps + 0.5)
        update_stars(page_num-2, page_num+1, True, ps)

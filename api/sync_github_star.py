import time, random
from concurrent.futures import ThreadPoolExecutor

import requests
from rag.utils.redis_conn import REDIS_CONN

tokens = [
    "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",#kevinhu
    "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",#naomi
    "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",#zhicang
    "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",#yongteng
    "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",#shengle
    "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",#zhenghang
    "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",#dcc
    "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",#liuan
]


def get_repo_stars():
    global tokens
    # GitHub API URL
    url = f"https://api.github.com/repos/infiniflow/ragflow"
    while True:
        headers = {
            "Authorization": f"Bearer %s"%random.choice(tokens),
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
    base_url = f"https://api.github.com/repos/infiniflow/ragflow/stargazers"

    page_num = 0
    page = to_page
    while True:
        headers = {
            "Authorization": f"Bearer %s"%random.choice(tokens),
            "Accept": "application/vnd.github.v3+json"
        }
        try:
            url = f"{base_url}?page={page}&per_page={page_size}"
            print(url)
            response = requests.get(url,headers=headers)
            response.raise_for_status()
            stargazers = response.json()
            if not stargazers and not desc:break
            for item in stargazers:
                REDIS_CONN.set(item["login"], 1, exp=36000)
                #print(item["login"], flush=True)
        except Exception as e:
            print(f"Update stars fail. Failed to retrieve star user information: {e}")
            time.sleep(30)
        page_num += 1
        if desc:
            page -= 1
            if page < from_page: break
        else:
            page += 1
            if page > from_page: break
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

#!/usr/bin/env python3

# PEP 723 metadata
# /// script
# requires-python = ">=3.10,<3.11"
# dependencies = [
#   "requests",
# ]
# ///

"""
https://paddlepaddle.github.io/PaddleOCR/latest/quick_start.html#python

$ python paddleocr_client.py det img_12.jpg
Response content: {"output":[[[[397.0,802.0],[1090.0,800.0],[1090.0,841.0],[397.0,843.0]],[[399.0,750.0],[1211.0,750.0],[1211.0,789.0],[399.0,789.0]],[[397.0,698.0],[1213.0,696.0],[1213.0,738.0],[397.0,739.0]],[[397.0,648.0],[1209.0,646.0],[1209.0,686.0],[397.0,688.0]],[[401.0,598.0],[1208.0,598.0],[1208.0,638.0],[401.0,638.0]],[[399.0,548.0],[1209.0,548.0],[1209.0,588.0],[399.0,588.0]],[[401.0,496.0],[1209.0,496.0],[1209.0,536.0],[401.0,536.0]],[[399.0,446.0],[1207.0,445.0],[1208.0,486.0],[399.0,488.0]],[[399.0,396.0],[1206.0,395.0],[1206.0,436.0],[399.0,438.0]],[[401.0,346.0],[1204.0,346.0],[1204.0,386.0],[401.0,386.0]],[[444.0,176.0],[1166.0,176.0],[1166.0,222.0],[444.0,222.0]]]]}

$ python paddleocr_client.py rec word_10.png
Response content: {"output":[[["PAIN",0.9906066656112671]]]}
"""

import sys
import base64
import requests

if len(sys.argv) != 3:
    print("Usage: paddleocr_client.py [det|rec] image_path")
    sys.exit(-1)

operation = sys.argv[1]
image_path = sys.argv[2]

with open(image_path, "rb") as image_file:
    image_bytes = image_file.read()

base64_encoded = base64.b64encode(image_bytes).decode("utf-8")

payload = {"image": base64_encoded, "operation": operation}

url = "http://localhost:8000/predict"
try:
    response = requests.post(url, json=payload)
    if response.status_code == 200:
        print("Response received successfully!")
        print("Response content:", response.text)
    else:
        print(f"Failed with status code: {response.status_code}")
        print("Response content:", response.text)
except requests.exceptions.RequestException as e:
    print(f"Error during request: {e}")

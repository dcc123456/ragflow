<p align="center">
<img src="https://github-production-user-asset-6210df.s3.amazonaws.com/12318111/435042067-e720c9ff-090f-469c-b886-e6e35f674b74.png?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=AKIAVCODYLSA53PQK4ZA%2F20250418%2Fus-east-1%2Fs3%2Faws4_request&X-Amz-Date=20250418T032317Z&X-Amz-Expires=300&X-Amz-Signature=17e11cac6b3c6cb4494b6fc24333fdd249417b7f0e773adbe050bc90b9dfc2c6&X-Amz-SignedHeaders=host" width="60%" align=""/>
</p>

# 先决条件

## 登录 10.29.35.44
```shell
ssh icbccs@10.29.35.44
```

### 资源：
```shell
docker commit infiniflow-ragflow-server infiniflow-ai/ragflow:latest
docker save -o infiniflow-ragflow-server.tar infiniflow-ai/ragflow:latest
scp infiniflow-ragflow-server.tar <目标机器>:/app/infiniflow-ai/
scp -r /app/infiniflow-ai/docker <目标机器>:/app/infiniflow-ai/
```
```shell
ssh <目标机器>
cd /app/infiniflow-ai/
docker load -i infiniflow-ragflow-server.tar
```


 
参见 /app/infiniflow-ai/docker/nginx/svr.conf。其中包含统一认证的地址和RAGFlow server分布机器的节点。
更改upstream的IP和端口。

## 单点组件

 - 复制镜像
```shell
docker save -o mysql.tar mysql:8.0.39
scp mysql.tar <目标机器>:/app/infiniflow-ai/

docker save -o valkey.tar valkey:8
scp valkey.tar <目标机器>:/app/infiniflow-ai/

docker save -o kibana.tar swr.cn-north-4.myhuaweicloud.com/ddn-k8s/docker.elastic.co/kibana/kibana:8.11.3
scp kibana.tar <目标机器>:/app/infiniflow-ai/

docker save -o nginx.tar nginx:1.26.1-alpine
scp nginx.tar <目标机器>:/app/infiniflow-ai/
```

 - 加载镜像
```shell
ssh <目标机器>
cd /app/infiniflow-ai/
docker load -i mysql.tar
docker load -i valkey.tar
docker load -i kibana.tar
docker load -i nginx.tar
```

 - 启动服务
1. /app/infiniflow-ai/docker/.env中定义了各个组件的用户名密码，这个文件中更改了内容需重启相关服务才能生效。
2. /app/infiniflow-ai/docker/nginx/es.conf中包含ES分布机器的节点，请先确认。
3. /app/infiniflow-ai/docker/.env中包含ELASTICSEARCH_HOSTS为ES的IP和端口，为Kibana所用，请先确认。

```shell
cd /app/infiniflow-ai/
docker-compose -f docker-compose-base.yml up mysql -d
docker-compose -f docker-compose-base.yml up redis -d
docker-compose -f docker-compose-base.yml up nginx-es -d
docker-compose -f docker-compose-base.yml up kibana -d
```

## 非单点组件

 - 复制镜像
```shell
docker save -o elasticsearch.tar elasticsearch:8.11.3
scp elasticsearch.tar <目标机器>:/app/infiniflow-ai/
docker save -o minio.tar quay.io/minio/minio:RELEASE.2023-12-20T01-00-02Z
scp minio.tar <目标机器>:/app/infiniflow-ai/
```

 - 加载镜像
```shell
ssh <目标机器>
cd /app/infiniflow-ai/
docker load -i elasticsearch.tar
docker load -i minio.tar
```

 - 启动服务
1. /app/infiniflow-ai/docker/.env中定义了各个组件的用户名密码，这个文件中更改了内容需重启相关服务才能生效。
2. 启动ES之前，请更改docker-compose-base.yml，每台机器上的这个文件关于ES的服务都不一样，将es01全局替换成相应编号，如：es02，es03.。。更改`discovery.seed_hosts`的IP，需排除本机IP。更改`initial_master_nodes`为所有节点IP。
3. 保证 vm.max_map_count ≥ 262144
```
sysctl vm.max_map_count
sudo sysctl -w vm.max_map_count=262144
sudo echo "vm.max_map_count=262144" >> /etc/sysctl.conf
```

```shell
cd /app/infiniflow-ai/
docker-compose -f docker-compose-base.yml up es -d
docker-compose -f docker-compose-base.yml up minio -d
```

### ES的安装中密钥的生成请参考(可选)：
```shell
/app/infiniflow-ai/docker/certs/gen.sh
/app/infiniflow-ai/docker/certs/instances.yml
```

### ES使用内存大小控制(可选)：
```shell
vim /app/infiniflow-ai/docker/docker-compose-base.yml
```
```yaml
    environment:
      - ES_JAVA_OPTS=-Xms12g -Xmx12g
      - JVM_OPTS=-Xms12g -Xmx12g

```


# RAGFlow server的启动。

 1. 确保`/app/infiniflow-ai/docker/service_conf.yaml`中各个组件的IP端口，用户名和密码配置正确（请与/app/infiniflow-ai/docker/.env中的记录核对）。请参考`44`上的文件。
 2. RAGFlow server的启动在`docker-compose.yml`中有定义。
 3. /app/infiniflow-ai/docker/.env中定义了各个组件的用户名密码，请于service_conf.yaml中的保持一致。其中`MAX_FILE_NUM_PER_USER`定义了每个租户通过界面能上传的文件限制。（重启生效）

```shell
cd /app/infiniflow-ai/docker/
docker-compose up
```

# Rebuild RAGFlow的镜像
> `/app/infiniflow-ai/ragflow/`目录下为闭源部分代码，`/app/infiniflow-ai/ragflow/oss`为开源部分代码。闭源代码覆盖开源代码后为完整代码。
```shell
cd /app/infiniflow-ai/
./rebuild.sh
```

# 模型服务的部署

一共有三个模型服务：OCR，TSR（表格结构识别），DLA（文档布局识别）
- 将`58`的容器commit到镜像后部署到目标机器。在目标机器上启动容器。
- 进入容器分别启动三个模型服务：
```shell
cd ~/deepdoc/ocr; CUDA_VISIBLE_DEVICES=0 python paddleocr_server.py > ~/logs/ocr.log 2>&1 &
cd ~/deepdoc/tsr; CUDA_VISIBLE_DEVICES=0 python tsr_svr.py  --engine tsr.trt  > ~/logs/tsr.log 2>&1 &
cd ~/deepdoc/dla; CUDA_VISIBLE_DEVICES=1 python dla_svr.py --engine dla.trt --port 3344  > ~/logs/dla.log 2>&1  &
```

## 模型服务推理API说明

## POST OCR

POST /predict

> Body 请求参数

```yaml
request: ""
operator: ""

```

### 请求参数

|名称|位置|类型|必选|说明|
|---|---|---|---|---|
|body|body|object| 否 |none|
|» request|body|string(binary)| 是 |none|
|» operator|body|string| 是 |none|

#### 枚举值

|属性|值|
|---|---|
|» operator|rec|
|» operator|det|

> 返回示例
> operator: "det", 返回为box的四个角坐标
```json
{
  "output": [
    [
      [
        [
          373,
          1628
        ],
        [
          852,
          1628
        ],
        [
          852,
          1655
        ],
        [
          373,
          1655
        ]
      ],
      [
        [
          373,
          1595
        ],
        [
          1267,
          1595
        ],
        [
          1267,
          1628
        ],
        [
          373,
          1628
        ]
      ]
    ]
  ]
}
```

> operator: "rec"，返回为识别到的文字和置信度。
```json
{
    "output": [
      [
        [
          "40k~60kbytes/s，中文约20k~45kbytes/s。", 0.9804858565330505
        ]
      ]
    ]
}
```

## POST TSR

POST /predict

> Body 请求参数

```yaml
files: ""

```

### 请求参数

|名称|位置|类型|必选|说明|
|---|---|---|---|---|
|body|body|object| 否 |none|
|» files|body|string(binary)| 是 |{"request": binary}|

> 返回示例
> 每个单元格的bbox的坐标（left/top/right/bottom）、置信度、类别（忽略）

```json
{
  "bboxes": [
    [
      826,
      240,
      1261,
      273,
      0.88,
      0
    ],
    [
      826,
      207,
      1261,
      240,
      0.87,
      0
    ],
    [
      369,
      207,
      828,
      240,
      0.869,
      0
    ]
  ]
}
```

## POST DLA

POST /predict

> Body 请求参数

```yaml
files: ""

```

### 请求参数

|名称|位置|类型|必选|说明|
|---|---|---|---|---|
|body|body|object| 否 |none|
|» files|body|string(binary)| 是 |{"request": binary}|

> 返回示例
> 每个单元格的bbox的坐标（left/top/right/bottom）、置信度、类别。
```json
类别种类：
[
    "title",
    "Text",
    "Reference",
    "Figure",
    "Figure caption",
    "Table",
    "Table caption",
    "Table caption",
    "Equation",
    "Figure caption",
]
```
```json
{
  "bboxes": [
    [
      826,
      240,
      1261,
      273,
      0.88,
      0
    ],
    [
      826,
      207,
      1261,
      240,
      0.87,
      0
    ],
    [
      369,
      207,
      828,
      240,
      0.869,
      0
    ]
  ]
}
```

            
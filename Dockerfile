FROM swr.cn-north-4.myhuaweicloud.com/infiniflow/ragflow-base:v1.0
USER  root

WORKDIR /ragflow

RUN mkdir /root/.ragflow/
RUN /root/miniconda3/envs/py11/bin/pip install python-calamine>=0.4.0
RUN /root/miniconda3/envs/py11/bin/pip install python3-saml pypdf==6.0.0
RUN /root/miniconda3/envs/py11/bin/pip uninstall -y lxml xmlsec
RUN /root/miniconda3/envs/py11/bin/pip install --no-cache-dir --force-reinstall lxml xmlsec
RUN /root/miniconda3/envs/py11/bin/pip install flask-mail>=0.10.0 flask_limiter
RUN /root/miniconda3/envs/py11/bin/pip install langfuse>=2.60.0 ultralytics
RUN /root/miniconda3/envs/py11/bin/pip install olefile

COPY web web
RUN cd ./web && npm i && npm run build

COPY conf conf
COPY docs docs
COPY plugin plugin
COPY graphrag graphrag
COPY deepdoc deepdoc
COPY agent agent
COPY rag rag
COPY api api
COPY agentic_reasoning agentic_reasoning
COPY mcp mcp
COPY admin admin

ENV PYTHONPATH=/ragflow/
ENV HF_ENDPOINT=https://hf-mirror.com

ADD docker/entrypoint.sh ./entrypoint.sh
RUN chmod +x ./entrypoint.sh
ADD docker/.env ./.env

ENTRYPOINT ["./entrypoint.sh"]

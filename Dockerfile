FROM swr.cn-north-4.myhuaweicloud.com/infiniflow/ragflow-base:v1.0
USER  root

WORKDIR /ragflow

RUN mkdir /root/.ragflow/
RUN /root/miniconda3/envs/py11/bin/pip install python-calamine>=0.4.0
RUN /root/miniconda3/envs/py11/bin/pip install python3-saml pypdf==6.0.0
RUN /root/miniconda3/envs/py11/bin/pip uninstall -y lxml xmlsec
RUN /root/miniconda3/envs/py11/bin/pip install --no-cache-dir --force-reinstall lxml xmlsec
RUN /root/miniconda3/envs/py11/bin/pip install flask-mail>=0.10.0 flask_limiter
RUN /root/miniconda3/envs/py11/bin/pip install langfuse>=2.60.0

ADD ./ragflow/oss/conf ./conf
COPY ./ragflow/conf ./conf


ADD ./ragflow/oss/web ./web
COPY ./ragflow/web ./web
ADD ./ragflow/oss/docs ./docs
RUN cd ./web && npm i && npm run build

ADD ./ragflow/oss/graphrag ./graphrag
ADD ./ragflow/oss/agentic_reasoning ./agentic_reasoning

ADD ./ragflow/oss/deepdoc ./deepdoc
COPY ./ragflow/deepdoc ./deepdoc/

ADD ./ragflow/oss/agent ./agent

ADD ./ragflow/oss/rag ./rag
COPY ./ragflow/rag ./rag/

ADD ./ragflow/oss/api ./api
COPY ./ragflow/api ./api/

ADD ./ragflow/oss/plugin ./plugin

ENV PYTHONPATH=/ragflow/
ENV HF_ENDPOINT=https://hf-mirror.com

ADD docker/entrypoint.sh ./entrypoint.sh
RUN chmod +x ./entrypoint.sh
ADD docker/.env ./.env

ENTRYPOINT ["./entrypoint.sh"]


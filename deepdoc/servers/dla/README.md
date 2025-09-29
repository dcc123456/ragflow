### 1. Install TensorRT

- Download [TensorRT](https://developer.nvidia.com/tensorrt/download) .
- Install
```shell
os="ubuntuxx04"
tag="10.x.x-cuda-x.x"
sudo dpkg -i nv-tensorrt-local-repo-${os}-${tag}_1.0-1_amd64.deb
sudo cp /var/nv-tensorrt-local-repo-${os}-${tag}/*-keyring.gpg /usr/share/keyrings/
sudo apt-get update

sudo apt-get install tensorrt

python3 -m pip install --upgrade tensorrt
python3 -m pip install tensorrt-cu11 tensorrt-lean-cu11 tensorrt-dispatch-cu11
```

```commandline
python3
>>> import tensorrt
>>> print(tensorrt.__version__)
>>> assert tensorrt.Builder(tensorrt.Logger())
```

### 2. Transform `.pt` to `.onnx`
```commandline
>>> from ultralytics import YOLOv10 as YOLO
>>> model = YOLO("./doclayout_yolo_docstructbench_imgsz1024.pt")
>>> model.export(format='onnx')
```

### 3. Transform `.onnx` to `.trt`
```shell
cd ragflow/deepdoc/servers/dla/yolov10_to_tensor
/app/.venv/bin/python3 export.py  -o ../../../../rag/res/deepdoc/layout.onnx -e ../layout.trt --end2end  -p fp16 --v10
```

### 4. Start server
```shell
cd ragflow/deepdoc/servers/dla/
ps aux|grep dla_svr|awk '{print $2}'|xargs kill -9;python dla_svr.py --engine layout.trt

```

### 4. Test client
```shell
cd deepdoc/vision
PYTHONPATH=/home/user/infiniflow-ai/ragflow/ python dla_cli.py --ip 0.0.0.0 --port 11234 --image ly.jpg
```

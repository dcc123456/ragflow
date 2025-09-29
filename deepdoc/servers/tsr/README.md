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

### 2. Transform `.pt` to `.engine`

```shell
cd yolov8_to_tensorrt
/app/.venv/bin/python3 export-det.py --weights ../yolov8x.pt --iou-thres 0.45 --conf-thres 0.4 --topk 1024 --opset 11 --sim  --input-shape 1 3 640 640  --device cuda:0
/app/.venv/bin/python3 build.py --weights ./yolov8x.onnx --iou-thres 0.45 --conf-thres 0.4 --topk 1024 --fp16 --device cuda:0
/app/.venv/bin/python3 infer.py --engine=./yolov8x.engine  --imgs ./images
```

### 3. Start server
```shell
ps aux|grep tsr_svr|awk '{print $2}'|xargs kill -9;python tsr_svr.py --engine yolov8x.engine

```

### 4. Test client
```shell
cd deepdoc/vision
python tsr_cli.py --ip 0.0.0.0 --port 11234 --image table.jpg
```
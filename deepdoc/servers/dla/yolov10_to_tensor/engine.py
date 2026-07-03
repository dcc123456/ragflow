import tensorrt as trt
from cuda import cudart
import numpy as np
import cv2

from .utils import cuda_call, memcpy_host_to_device, memcpy_device_to_host, vis, letterbox, multiclass_nms


class BaseEngine(object):
    def __init__(self, engine_path):
        self.mean = None
        self.std = None
        self.n_classes = 80
        self.class_names = [
            "person",
            "bicycle",
            "car",
            "motorcycle",
            "airplane",
            "bus",
            "train",
            "truck",
            "boat",
            "traffic light",
            "fire hydrant",
            "stop sign",
            "parking meter",
            "bench",
            "bird",
            "cat",
            "dog",
            "horse",
            "sheep",
            "cow",
            "elephant",
            "bear",
            "zebra",
            "giraffe",
            "backpack",
            "umbrella",
            "handbag",
            "tie",
            "suitcase",
            "frisbee",
            "skis",
            "snowboard",
            "sports ball",
            "kite",
            "baseball bat",
            "baseball glove",
            "skateboard",
            "surfboard",
            "tennis racket",
            "bottle",
            "wine glass",
            "cup",
            "fork",
            "knife",
            "spoon",
            "bowl",
            "banana",
            "apple",
            "sandwich",
            "orange",
            "broccoli",
            "carrot",
            "hot dog",
            "pizza",
            "donut",
            "cake",
            "chair",
            "couch",
            "potted plant",
            "bed",
            "dining table",
            "toilet",
            "tv",
            "laptop",
            "mouse",
            "remote",
            "keyboard",
            "cell phone",
            "microwave",
            "oven",
            "toaster",
            "sink",
            "refrigerator",
            "book",
            "clock",
            "vase",
            "scissors",
            "teddy bear",
            "hair drier",
            "toothbrush",
        ]

        logger = trt.Logger(trt.Logger.WARNING)
        logger.min_severity = trt.Logger.Severity.ERROR
        runtime = trt.Runtime(logger)
        trt.init_libnvinfer_plugins(logger, "")  # initialize TensorRT plugins
        with open(engine_path, "rb") as f:
            serialized_engine = f.read()
        self.engine = runtime.deserialize_cuda_engine(serialized_engine)
        self.imgsz = self.engine.get_tensor_shape(self.engine.get_tensor_name(0))[2:]  # get the read shape of model, in case user input it wrong
        self.context = self.engine.create_execution_context()
        # Setup I/O bindings
        self.inputs = []
        self.outputs = []
        self.allocations = []
        for i in range(self.engine.num_io_tensors):
            name = self.engine.get_tensor_name(i)
            dtype = self.engine.get_tensor_dtype(name)
            shape = self.engine.get_tensor_shape(name)
            is_input = False
            if self.engine.get_tensor_mode(name) == trt.TensorIOMode.INPUT:
                is_input = True
            if is_input:
                self.batch_size = shape[0]
            size = np.dtype(trt.nptype(dtype)).itemsize
            for s in shape:
                size *= s
            allocation = cuda_call(cudart.cudaMalloc(size))
            binding = {"index": i, "name": name, "dtype": np.dtype(trt.nptype(dtype)), "shape": list(shape), "allocation": allocation, "size": size}
            self.allocations.append(allocation)
            if self.engine.get_tensor_mode(name) == trt.TensorIOMode.INPUT:
                self.inputs.append(binding)
            else:
                self.outputs.append(binding)

    def output_spec(self):
        """
        Get the specs for the output tensors of the network. Useful to prepare memory allocations.
        :return: A list with two items per element, the shape and (numpy) datatype of each output tensor.
        """
        specs = []
        for o in self.outputs:
            specs.append((o["shape"], o["dtype"]))
        return specs

    def infer(self, img):
        """
        Execute inference on a batch of images. The images should already be batched and preprocessed, as prepared by
        the ImageBatcher class. Memory copying to and from the GPU device will be performed here.
        :param batch: A numpy array holding the image batch.
        :param scales: The image resize scales for each image in this batch. Default: No scale postprocessing applied.
        :return: A nested list for each image in the batch and each detection in the list.
        """

        # Prepare the output data.
        outputs = []
        for shape, dtype in self.output_spec():
            outputs.append(np.zeros(shape, dtype))

        # Process I/O and execute the network.
        memcpy_host_to_device(self.inputs[0]["allocation"], np.ascontiguousarray(img))

        self.context.execute_v2(self.allocations)
        for o in range(len(outputs)):
            memcpy_device_to_host(outputs[o], self.outputs[o]["allocation"])
        return outputs

    def inference(self, img_path, conf=0.5, end2end=False):
        origin_img = cv2.imread(img_path)
        # img, ratio = preproc(origin_img, self.imgsz, self.mean, self.std)
        img, ratio, dwdh = letterbox(origin_img, self.imgsz)
        data = self.infer(img)
        if end2end:
            num, final_boxes, final_scores, final_cls_inds = data
            # final_boxes, final_scores, final_cls_inds  = data
            dwdh = np.asarray(dwdh * 2, dtype=np.float32)
            final_boxes -= dwdh
            final_boxes = np.reshape(final_boxes / ratio, (-1, 4))
            final_scores = np.reshape(final_scores, (-1, 1))
            final_cls_inds = np.reshape(final_cls_inds, (-1, 1))
            dets = np.concatenate([np.array(final_boxes)[: int(num[0])], np.array(final_scores)[: int(num[0])], np.array(final_cls_inds)[: int(num[0])]], axis=-1)
        else:
            predictions = np.reshape(data, (1, -1, int(5 + self.n_classes)))[0]
            dets = self.postprocess(predictions, ratio)

        if dets is not None:
            final_boxes, final_scores, final_cls_inds = dets[:, :4], dets[:, 4], dets[:, 5]
            origin_img = vis(origin_img, final_boxes, final_scores, final_cls_inds, conf=conf, class_names=self.class_names)
        return origin_img

    @staticmethod
    def postprocess(predictions, ratio):
        boxes = predictions[:, :4]
        scores = predictions[:, 4:5] * predictions[:, 5:]
        boxes_xyxy = np.ones_like(boxes)
        boxes_xyxy[:, 0] = boxes[:, 0] - boxes[:, 2] / 2.0
        boxes_xyxy[:, 1] = boxes[:, 1] - boxes[:, 3] / 2.0
        boxes_xyxy[:, 2] = boxes[:, 0] + boxes[:, 2] / 2.0
        boxes_xyxy[:, 3] = boxes[:, 1] + boxes[:, 3] / 2.0
        boxes_xyxy /= ratio
        dets = multiclass_nms(boxes_xyxy, scores, nms_thr=0.45, score_thr=0.1)
        return dets

    def get_fps(self):
        import time

        img = np.ones((1, 3, self.imgsz[0], self.imgsz[1]))
        img = np.ascontiguousarray(img, dtype=np.float32)
        for _ in range(5):  # warmup
            _ = self.infer(img)

        t0 = time.perf_counter()
        for _ in range(100):  # calculate average time
            _ = self.infer(img)
        print(100 / (time.perf_counter() - t0), "FPS")

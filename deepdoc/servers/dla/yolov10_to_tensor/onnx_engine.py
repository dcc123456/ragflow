"""ONNX Runtime engine wrapper for CPU inference"""

import numpy as np
import onnxruntime as ort


class ONNXEngine:
    """ONNX Runtime engine for CPU inference

    Compatible API with TensorRT BaseEngine for drop-in replacement
    """

    def __init__(self, model_path: str):
        self.model_path = model_path

        # Create ONNX Runtime session with CPU provider
        self.session = ort.InferenceSession(model_path, providers=["CPUExecutionProvider"], sess_options=self._create_session_options())

        # Get model info
        self.input_name = self.session.get_inputs()[0].name
        input_shape = self.session.get_inputs()[0].shape
        self.imgsz = (input_shape[2], input_shape[3])  # (height, width)

        # Get output names
        self.output_names = [o.name for o in self.session.get_outputs()]

        # Warm up
        self._warmup()

    def _create_session_options(self):
        """Create session options for better performance"""
        so = ort.SessionOptions()
        so.graph_optimization_level = ort.GraphOptimizationLevel.ORT_ENABLE_ALL
        so.execution_mode = ort.ExecutionMode.ORT_SEQUENTIAL
        return so

    def _warmup(self):
        """Warm up the model with dummy input"""
        dummy_input = np.zeros((1, 3, *self.imgsz), dtype=np.float32)
        self.session.run(self.output_names, {self.input_name: dummy_input})

    def infer(self, img):
        """Run inference on preprocessed image tensor

        Args:
            img: Preprocessed image tensor (1, 3, H, W) float32 in range [0, 1]

        Returns:
            List of output arrays from the model
        """
        outputs = self.session.run(self.output_names, {self.input_name: img})
        return outputs

    def __call__(self, img):
        """Allow calling engine like a function"""
        return self.infer(img)

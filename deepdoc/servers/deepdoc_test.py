#!/usr/bin/env python3
"""
DeepDoc Service Test Client

Test script to verify DLA, OCR, and TSR services are working correctly.
"""
import io
import sys
import time

import requests
from PIL import Image, ImageDraw, ImageFont


class DeepDocClient:
    """Client for testing DeepDoc unified server"""

    def __init__(self, base_url: str = "http://localhost:8000"):
        self.base_url = base_url
        self.endpoints = {
            'dla': f'{base_url}/predict/dla',
            'ocr': f'{base_url}/predict/ocr',
            'tsr': f'{base_url}/predict/tsr',
        }

    def check_server_health(self):
        """Check if server is running"""
        try:
            response = requests.get(f"{self.base_url}/health", timeout=5)
            print(f"✓ Server health check: {response.status_code}")
            return True
        except requests.exceptions.RequestException as e:
            print(f"✗ Server health check failed: {e}")
            print("  Make sure the server is running")
            return False

    def create_test_image(self, width: int = 640, height: int = 480):
        """Create a simple test image with text and shapes"""
        # Create white background
        img = Image.new('RGB', (width, height), color='white')
        draw = ImageDraw.Draw(img)

        # Draw some rectangles (simulating document layout)
        draw.rectangle([50, 50, 200, 100], fill='lightblue', outline='blue')
        draw.rectangle([50, 120, 590, 200], fill='lightgray', outline='gray')

        # Add text
        try:
            font = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf", 24)
        except Exception:
            try:
                font = ImageFont.truetype("arial.ttf", 24)
            except Exception:
                font = ImageFont.load_default()

        draw.text((60, 60), "Title Test", fill='black', font=font)
        draw.text((60, 140), "This is a test document for OCR evaluation.", fill='black', font=font)

        # Draw a simple table structure
        for i in range(4):
            y = 220 + i * 40
            draw.line([50, y, 590, y], fill='black', width=2)
        draw.line([200, 220, 200, 380], fill='black', width=2)
        draw.line([400, 220, 400, 380], fill='black', width=2)

        return self.pil_to_bytes(img)

    def create_hello_world_image(self, width: int = 400, height: int = 100):
        """Create a simple test image with 'Hello World' text"""
        # Create white background
        img = Image.new('RGB', (width, height), color='white')
        draw = ImageDraw.Draw(img)

        # Try to use a better font, fallback to default if not available
        try:
            font = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf", 36)
        except Exception:
            try:
                font = ImageFont.truetype("arial.ttf", 36)
            except Exception:
                font = ImageFont.load_default()

        # Draw "Hello World" text centered
        text = "Hello World"
        bbox = draw.textbbox((0, 0), text, font=font)
        text_width = bbox[2] - bbox[0]
        text_height = bbox[3] - bbox[1]

        # Center the text
        x = (width - text_width) // 2
        y = (height - text_height) // 2

        draw.text((x, y), text, fill='black', font=font)

        # Save a copy for debugging
        img.save('/tmp/hello_world_test.png')
        print("✓ Test image saved to /tmp/hello_world_test.png")

        return self.pil_to_bytes(img)

    def create_table_image(self, width: int = 640, height: int = 480):
        """Create a test image with a table structure"""
        img = Image.new('RGB', (width, height), color='white')
        draw = ImageDraw.Draw(img)

        # Draw table title
        try:
            font = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf", 24)
        except Exception:
            font = ImageFont.load_default()

        draw.text((220, 30), "Sample Table", fill='black', font=font)

        # Draw table grid
        table_top = 80
        table_left = 50
        table_right = 590
        row_height = 50
        num_rows = 6
        num_cols = 4

        # Draw horizontal lines
        for i in range(num_rows + 1):
            y = table_top + i * row_height
            draw.line([table_left, y, table_right, y], fill='black', width=2)

        # Draw vertical lines
        col_width = (table_right - table_left) // num_cols
        for i in range(num_cols + 1):
            x = table_left + i * col_width
            draw.line([x, table_top, x, table_top + num_rows * row_height], fill='black', width=2)

        # Add some cell content
        small_font = ImageFont.load_default()
        cells = [
            (0, 0, "Name"), (1, 0, "Age"), (2, 0, "City"),
            (0, 1, "Alice"), (1, 1, "25"), (2, 1, "NYC"),
            (0, 2, "Bob"), (1, 2, "30"), (2, 2, "LA"),
        ]
        for col, row, text in cells:
            x = table_left + col * col_width + 10
            y = table_top + row * row_height + 15
            draw.text((x, y), text, fill='black', font=small_font)

        return self.pil_to_bytes(img)

    def pil_to_bytes(self, image: Image.Image) -> bytes:
        """Convert PIL Image to bytes"""
        buffer = io.BytesIO()
        image.save(buffer, format='PNG')
        return buffer.getvalue()

    def test_dla(self):
        """Test Document Layout Analysis endpoint"""
        print("\n" + "="*60)
        print("Testing DLA (Document Layout Analysis)")
        print("="*60)

        # Generate simple Hello World test image (more reliable for DLA detection)
        print("Generating 'Hello World' test image for layout analysis...")
        image_bytes = self.create_hello_world_image()

        # Expected image dimensions: 400x100
        expected_img_width, expected_img_height = 400, 100
        # Expected text location: centered, approximately 100-300 width, 30-70 height
        expected_text_bbox = {
            'x_min': 80,   # Allow some margin
            'y_min': 30,
            'x_max': 320,
            'y_max': 70
        }

        try:
            files = {'request': ('test.png', io.BytesIO(image_bytes), 'image/png')}
            response = requests.post(self.endpoints['dla'], files=files, timeout=30)

            if response.status_code == 200:
                result = response.json()
                bboxes = result.get('bboxes', [])

                print("✓ DLA request successful")
                print(f"  Detected {len(bboxes)} layout element(s)")

                if bboxes:
                    print("  Sample detections (format: [x_min, y_min, x_max, y_max, label_score, label_id]):")
                    for i, bbox in enumerate(bboxes[:3]):
                        # Format: [x_min, y_min, x_max, y_max, label_score, label_id]
                        x_min, y_min, x_max, y_max = bbox[0], bbox[1], bbox[2], bbox[3]
                        label_score, label_id = bbox[4], bbox[5]
                        print(f"    [{i}] Box: ({x_min:.1f}, {y_min:.1f}, {x_max:.1f}, {y_max:.1f}), "
                              f"Label Score: {label_score:.3f}, Label ID: {label_id}")
                    if len(bboxes) > 3:
                        print(f"    ... and {len(bboxes) - 3} more")

                    # Verify detection quality
                    print("\n  Validating detection quality...")
                    success = True

                    for i, bbox in enumerate(bboxes):
                        x_min, y_min, x_max, y_max = bbox[0], bbox[1], bbox[2], bbox[3]
                        label_score = bbox[4]

                        # Check 1: Bounding box should be within image bounds
                        if x_min < 0 or y_min < 0 or x_max > expected_img_width or y_max > expected_img_height:
                            print(f"    ✗ Detection [{i}]: Box extends beyond image bounds")
                            success = False
                            continue

                        # Check 2: Bounding box should overlap with expected text region
                        overlap_x = max(0, min(x_max, expected_text_bbox['x_max']) -
                                       max(x_min, expected_text_bbox['x_min']))
                        overlap_y = max(0, min(y_max, expected_text_bbox['y_max']) -
                                       max(y_min, expected_text_bbox['y_min']))

                        if overlap_x > 0 and overlap_y > 0:
                            # Has overlap with expected region
                            pass
                        else:
                            print(f"    ⚠ Detection [{i}]: No overlap with expected text region")

                        # Check 3: Label score should be reasonable (> 0.2)
                        if label_score < 0.2:
                            print(f"    ⚠ Detection [{i}]: Low label score ({label_score:.3f})")

                        # Check 4: Bounding box should have reasonable size
                        bbox_width = x_max - x_min
                        bbox_height = y_max - y_min
                        if bbox_width < 50 or bbox_height < 20:
                            print(f"    ⚠ Detection [{i}]: Unusually small box ({bbox_width:.1f}x{bbox_height:.1f})")
                        elif bbox_width > expected_img_width * 0.8 or bbox_height > expected_img_height * 0.8:
                            print(f"    ⚠ Detection [{i}]: Unusually large box ({bbox_width:.1f}x{bbox_height:.1f})")

                    if success and len(bboxes) > 0:
                        print(f"  ✓ SUCCESS: DLA successfully detected {len(bboxes)} layout element(s) with valid bounding boxes!")
                        return True
                    else:
                        print("  ✗ FAIL: DLA detections failed validation")
                        return False
                else:
                    print("\n  ✗ FAIL: No layout elements detected")
                    return False
            else:
                print(f"✗ DLA request failed with status {response.status_code}")
                print(f"  Response: {response.text[:200]}")
                return False

        except requests.exceptions.RequestException as e:
            print(f"✗ DLA request failed: {e}")
            return False

    def test_ocr(self):
        """Test OCR endpoint with Hello World image"""
        print("\n" + "="*60)
        print("Testing OCR (Text Detection & Recognition)")
        print("="*60)

        # Generate Hello World test image
        print("Generating 'Hello World' test image...")
        image_bytes = self.create_hello_world_image()

        expected_text = "Hello World"

        try:
            # Using multipart/form-data as expected by the server
            files = {'request': ('test.png', io.BytesIO(image_bytes), 'image/png')}
            data = {'operator': 'rec'}  # Recognition mode

            response = requests.post(self.endpoints['ocr'], files=files, data=data, timeout=30)

            if response.status_code == 200:
                result = response.json()
                print("✓ OCR request successful")

                # Debug: print full response
                print(f"  Full response: {result}")

                # Extract text from response
                extracted_texts = []

                # Handle different response formats
                if 'output' in result:
                    output = result['output']
                    # Parse nested structure from PaddleOCR
                    # Format: [[[['text', confidence]]]]
                    if isinstance(output, list):
                        for item in output:
                            if isinstance(item, list):
                                for subitem in item:
                                    if isinstance(subitem, list):
                                        for text_item in subitem:
                                            # Handle both [text, conf] and (text, conf) formats
                                            if isinstance(text_item, (list, tuple)) and len(text_item) >= 2:
                                                text = text_item[0]
                                                confidence = text_item[1]
                                                if text:
                                                    extracted_texts.append((text, confidence))

                elif 'boxes' in result:
                    boxes = result['boxes']
                    for box in boxes:
                        if isinstance(box, dict) and 'text' in box:
                            extracted_texts.append((box['text'], box.get('confidence', 0.0)))
                        elif isinstance(box, (list, tuple)) and len(box) > 1:
                            extracted_texts.append((str(box[0]), float(box[1]) if isinstance(box[1], (int, float)) else 0.0))

                # Display results
                if extracted_texts:
                    print(f"  Detected {len(extracted_texts)} text region(s):")
                    for i, (text, conf) in enumerate(extracted_texts):
                        print(f"    [{i}] '{text}' (confidence: {conf:.2f})")

                    # Verify expected text
                    print(f"\n  Verifying expected text: '{expected_text}'")
                    all_texts = ' '.join([t for t, _ in extracted_texts])
                    if expected_text.lower() in all_texts.lower():
                        print(f"  ✓ SUCCESS: Expected text '{expected_text}' found in OCR result!")
                        return True
                    else:
                        print(f"  ✗ FAIL: Expected text '{expected_text}' NOT found")
                        print(f"  Detected text: '{all_texts}'")
                        return False
                else:
                    print("  ✗ No text detected")
                    print(f"  ✗ FAIL: Expected text '{expected_text}' not found")
                    return False

            else:
                print(f"✗ OCR request failed with status {response.status_code}")
                print(f"  Response: {response.text[:200]}")
                return False

        except requests.exceptions.RequestException as e:
            print(f"✗ OCR request failed: {e}")
            return False

    def test_tsr(self):
        """Test Table Structure Recognition endpoint"""
        print("\n" + "="*60)
        print("Testing TSR (Table Structure Recognition)")
        print("="*60)

        # Generate table test image
        print("Generating table test image...")
        image_bytes = self.create_table_image()

        # Table structure from create_table_image():
        # Image size: 640x480
        # Table: 6 rows x 4 columns
        # table_top = 80, table_left = 50, table_right = 590
        # row_height = 50, num_rows = 6, num_cols = 4
        # Expected: horizontal lines (7), vertical lines (5) = 12 table grid lines
        # Plus cell boxes = 24 cells
        expected_img_width, expected_img_height = 640, 480
        expected_table_region = {
            'x_min': 40,
            'y_min': 70,
            'x_max': 600,
            'y_max': 400
        }
        min_expected_detections = 15  # At least should detect table structure elements

        try:
            files = {'request': ('test.png', io.BytesIO(image_bytes), 'image/png')}
            response = requests.post(self.endpoints['tsr'], files=files, timeout=30)

            if response.status_code == 200:
                result = response.json()
                bboxes = result.get('bboxes', [])

                print("✓ TSR request successful")
                print(f"  Detected {len(bboxes)} table elements")

                if bboxes:
                    print("  Sample detections (format: [x_min, y_min, x_max, y_max, confidence, label_id]):")
                    for i, bbox in enumerate(bboxes[:3]):
                        # Format: [x_min, y_min, x_max, y_max, confidence, label_id]
                        if len(bbox) >= 6:
                            x_min, y_min, x_max, y_max = bbox[0], bbox[1], bbox[2], bbox[3]
                            confidence = bbox[4]
                            label_id = bbox[5]
                            print(f"    [{i}] Box: ({x_min:.1f}, {y_min:.1f}, {x_max:.1f}, {y_max:.1f}), "
                                  f"Confidence: {confidence:.3f}, Label: {label_id}")
                        else:
                            print(f"    [{i}] {bbox}")
                    if len(bboxes) > 3:
                        print(f"    ... and {len(bboxes) - 3} more")

                    # Verify detection quality
                    print("\n  Validating table structure detection...")
                    success = True
                    detections_in_table_region = 0

                    for i, bbox in enumerate(bboxes):
                        if len(bbox) < 6:
                            continue

                        x_min, y_min, x_max, y_max = bbox[0], bbox[1], bbox[2], bbox[3]
                        confidence = bbox[4]

                        # Check 1: Bounding box should be within image bounds
                        if x_min < 0 or y_min < 0 or x_max > expected_img_width or y_max > expected_img_height:
                            print(f"    ✗ Detection [{i}]: Box extends beyond image bounds")
                            success = False
                            continue

                        # Check 2: Bounding box should be within or overlap with expected table region
                        overlap_x = max(0, min(x_max, expected_table_region['x_max']) -
                                       max(x_min, expected_table_region['x_min']))
                        overlap_y = max(0, min(y_max, expected_table_region['y_max']) -
                                       max(y_min, expected_table_region['y_min']))

                        if overlap_x > 0 and overlap_y > 0:
                            detections_in_table_region += 1

                        # Check 3: Confidence should be reasonable (> 0.3 for table elements)
                        if confidence < 0.3:
                            print(f"    ⚠ Detection [{i}]: Low confidence ({confidence:.3f})")

                        # Check 4: Bounding box should have reasonable size (table cells/lines)
                        bbox_width = x_max - x_min
                        bbox_height = y_max - y_min
                        if bbox_width < 20 or bbox_height < 10:
                            print(f"    ⚠ Detection [{i}]: Very small box ({bbox_width:.1f}x{bbox_height:.1f})")

                    # Check 5: Should detect minimum number of table elements
                    if len(bboxes) < min_expected_detections:
                        print(f"    ⚠ Detected {len(bboxes)} elements, expected at least {min_expected_detections}")
                        success = False

                    # Check 6: Most detections should be in table region
                    if detections_in_table_region < len(bboxes) * 0.7:
                        print(f"    ⚠ Only {detections_in_table_region}/{len(bboxes)} detections in table region")

                    if success and len(bboxes) >= min_expected_detections:
                        print(f"  ✓ SUCCESS: TSR successfully detected {len(bboxes)} table structure elements!")
                        print(f"    - {detections_in_table_region} elements in expected table region")
                        print(f"    - Minimum requirement: {min_expected_detections} elements")
                        return True
                    else:
                        print("  ✗ FAIL: TSR detections failed validation")
                        return False
                else:
                    print("\n  ✗ FAIL: No table elements detected")
                    return False
            else:
                print(f"✗ TSR request failed with status {response.status_code}")
                print(f"  Response: {response.text[:200]}")
                return False

        except requests.exceptions.RequestException as e:
            print(f"✗ TSR request failed: {e}")
            return False

    def run_all_tests(self):
        """Run all tests"""
        print("="*60)
        print("DeepDoc Service Test Client")
        print("="*60)
        print(f"Server URL: {self.base_url}")

        # Check server health
        if not self.check_server_health():
            print("\n✗ Cannot proceed - server not available")
            return False

        # Run tests
        results = {}

        results['dla'] = self.test_dla()
        time.sleep(1)  # Small delay between tests

        results['ocr'] = self.test_ocr()
        time.sleep(1)

        results['tsr'] = self.test_tsr()

        # Summary
        print("\n" + "="*60)
        print("Test Summary")
        print("="*60)

        for service, passed in results.items():
            status = "✓ PASS" if passed else "✗ FAIL"
            print(f"  {service.upper()}: {status}")

        all_passed = all(results.values())
        if all_passed:
            print("\n✓ All tests passed!")
            return True
        else:
            print("\n✗ Some tests failed")
            return False


def main():
    import argparse

    parser = argparse.ArgumentParser(
        description="Test DeepDoc unified server (DLA, OCR, TSR)"
    )
    parser.add_argument(
        '--url',
        type=str,
        default='http://localhost:8000',
        help='Server URL (default: http://localhost:8000)'
    )
    parser.add_argument(
        '--service',
        type=str,
        choices=['dla', 'ocr', 'tsr', 'all'],
        default='all',
        help='Service to test (default: all)'
    )

    args = parser.parse_args()

    client = DeepDocClient(args.url)

    if args.service == 'all':
        success = client.run_all_tests()
    else:
        # Single service test
        print("="*60)
        print(f"Testing {args.service.upper()} service")
        print("="*60)

        if not client.check_server_health():
            print("\n✗ Cannot proceed - server not available")
            sys.exit(1)

        # Run specific test (each test generates its own image)
        if args.service == 'dla':
            success = client.test_dla()
        elif args.service == 'ocr':
            success = client.test_ocr()
        elif args.service == 'tsr':
            success = client.test_tsr()

    sys.exit(0 if success else 1)


if __name__ == '__main__':
    main()

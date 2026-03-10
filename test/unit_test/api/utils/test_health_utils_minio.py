#
#  Copyright 2025 The InfiniFlow Authors. All Rights Reserved.
#
#  Licensed under the Apache License, Version 2.0 (the "License");
#  you may not use this file except in compliance with the License.
#  You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
#  Unless required by applicable law or agreed to in writing, software
#  distributed under the License is distributed on an "AS IS" BASIS,
#  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#  See the License for the specific language governing permissions and
#  limitations under the License.
#
"""Unit tests for MinIO health check (check_minio_alive)."""
from unittest.mock import patch, Mock


class TestCheckMinioAlive:
    """Test check_minio_alive with mocked requests and settings."""

    @patch("api.utils.health_utils.requests.get")
    @patch("api.utils.health_utils.settings")
    def test_returns_alive_when_http_200(self, mock_settings, mock_get):
        mock_settings.MINIO = [{"host": "minio:9000"}]
        mock_response = Mock()
        mock_response.status_code = 200
        mock_get.return_value = mock_response
        from api.utils.health_utils import check_minio_alive
        result = check_minio_alive()
        assert result["status"] == "alive"
        assert "elapsed" in result["message"]
        mock_get.assert_called_once_with("http://minio:9000/minio/health/live")

    @patch("api.utils.health_utils.requests.get")
    @patch("api.utils.health_utils.settings")
    def test_returns_timeout_on_non_200(self, mock_settings, mock_get):
        mock_settings.MINIO = [{"host": "minio:9000"}]
        mock_response = Mock()
        mock_response.status_code = 503
        mock_get.return_value = mock_response
        from api.utils.health_utils import check_minio_alive
        result = check_minio_alive()
        assert result["status"] == "timeout"

    @patch("api.utils.health_utils.requests.get")
    @patch("api.utils.health_utils.settings")
    def test_returns_timeout_on_request_exception(self, mock_settings, mock_get):
        mock_settings.MINIO = [{"host": "minio:9000"}]
        mock_get.side_effect = ConnectionError("Connection refused")
        from api.utils.health_utils import check_minio_alive
        result = check_minio_alive()
        assert result["status"] == "timeout"
        assert "error" in result["message"]

    @patch("api.utils.health_utils.requests.get")
    @patch("api.utils.health_utils.settings")
    def test_uses_first_minio_node_in_list(self, mock_settings, mock_get):
        mock_settings.MINIO = [
            {"host": "minio-a:9000"},
            {"host": "minio-b:9000"},
        ]
        mock_response = Mock()
        mock_response.status_code = 200
        mock_get.return_value = mock_response
        from api.utils.health_utils import check_minio_alive
        check_minio_alive()
        mock_get.assert_called_once_with("http://minio-a:9000/minio/health/live")

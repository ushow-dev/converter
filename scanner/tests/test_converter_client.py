# scanner/tests/test_converter_client.py
from unittest.mock import patch, Mock
import requests as _requests
from scanner.api.converter_client import ConverterClient

BASE_URL = "http://converter:8000"
TOKEN = "secret"


def _client():
    return ConverterClient(base_url=BASE_URL, service_token=TOKEN)


def test_register_success():
    mock_resp = Mock()
    mock_resp.status_code = 200
    mock_resp.json.return_value = {"id": 42, "status": "new"}
    mock_resp.raise_for_status = Mock()
    with patch("requests.Session.post", return_value=mock_resp) as mock_post:
        item_id = _client().register(
            source_path="/incoming/Movie.mkv",
            source_filename="Movie.mkv",
            content_kind="movie",
        )
    assert item_id == 42


def test_register_raises_on_http_error():
    mock_resp = Mock()
    mock_resp.raise_for_status.side_effect = _requests.HTTPError("500")
    with patch("requests.Session.post", return_value=mock_resp):
        try:
            _client().register(source_path="/incoming/Movie.mkv", source_filename="Movie.mkv", content_kind="movie")
            assert False, "should raise"
        except _requests.HTTPError:
            pass


def test_get_status_completed():
    mock_resp = Mock()
    mock_resp.raise_for_status = Mock()
    mock_resp.json.return_value = {"id": 42, "status": "completed", "error_message": None}
    with patch("requests.Session.get", return_value=mock_resp):
        status, error = _client().get_status(42)
    assert status == "completed"
    assert error is None


def test_get_status_failed():
    mock_resp = Mock()
    mock_resp.raise_for_status = Mock()
    mock_resp.json.return_value = {"id": 42, "status": "failed", "error_message": "rclone error"}
    with patch("requests.Session.get", return_value=mock_resp):
        status, error = _client().get_status(42)
    assert status == "failed"
    assert error == "rclone error"


def test_get_status_network_error_raises():
    with patch("requests.Session.get", side_effect=_requests.RequestException("timeout")):
        try:
            _client().get_status(42)
            assert False, "should raise"
        except _requests.RequestException:
            pass

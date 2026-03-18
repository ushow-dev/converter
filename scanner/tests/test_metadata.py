# scanner/tests/test_metadata.py
from unittest.mock import patch, Mock
import requests as _requests
from scanner.services.metadata import (
    parse_filename,
    build_normalized_name,
    quality_label_from_release_type,
    tmdb_search,
)


def test_parse_filename_with_year():
    result = parse_filename("Doctor.Bakshi.2023.1080p.WEBRip.mkv")
    assert result["title"].lower() == "doctor bakshi"
    assert result["year"] == 2023


def test_parse_filename_without_year():
    result = parse_filename("SomeMovie.mkv")
    assert result["title"]
    assert result["year"] is None


def test_build_normalized_name_with_tmdb():
    assert build_normalized_name("Doctor Bakshi", 2023, "881935") == "doctor_bakshi_2023_[881935]"


def test_build_normalized_name_without_tmdb():
    assert build_normalized_name("Doctor Bakshi", 2023, None) == "doctor_bakshi_2023"


def test_build_normalized_name_without_year():
    assert build_normalized_name("Doctor Bakshi", None, None) == "doctor_bakshi"


def test_quality_label_webrip():
    assert quality_label_from_release_type("WEBRip") == "HD"


def test_quality_label_bluray():
    assert quality_label_from_release_type("Blu-ray") == "HD"


def test_quality_label_cam():
    assert quality_label_from_release_type("CAM") == "SD"


def test_quality_label_ts():
    assert quality_label_from_release_type("TS") == "SD"


def test_quality_label_unknown():
    assert quality_label_from_release_type("UNKNOWN") is None


def test_quality_label_none():
    assert quality_label_from_release_type(None) is None


def test_tmdb_search_success():
    mock_resp = Mock()
    mock_resp.status_code = 200
    mock_resp.json.return_value = {
        "results": [{"id": 881935, "title": "Doctor Bakshi", "release_date": "2023-01-01", "poster_path": "/poster.jpg"}]
    }
    with patch("requests.get", return_value=mock_resp):
        result = tmdb_search("Doctor Bakshi", 2023, "fake_key")
    assert result["tmdb_id"] == "881935"
    assert result["title"] == "Doctor Bakshi"


def test_tmdb_search_no_results():
    mock_resp = Mock()
    mock_resp.status_code = 200
    mock_resp.json.return_value = {"results": []}
    with patch("requests.get", return_value=mock_resp):
        assert tmdb_search("UnknownMovie", 2099, "fake_key") is None


def test_tmdb_search_network_error():
    with patch("requests.get", side_effect=_requests.RequestException("timeout")):
        assert tmdb_search("Doctor Bakshi", 2023, "fake_key") is None

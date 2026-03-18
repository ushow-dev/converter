# scanner/tests/test_quality.py
import json
from scanner.services.quality import compute_quality_score, parse_ffprobe_output


def test_1080p_h264_sdr():
    score = compute_quality_score(width=1920, height=1080, hdr=None, codec="h264", bitrate_kbps=8000)
    assert 40 <= score <= 50


def test_2160p_hevc_hdr10_high_bitrate():
    score = compute_quality_score(width=3840, height=2160, hdr="HDR10", codec="hevc", bitrate_kbps=40000)
    assert score >= 78


def test_720p_h264_sdr():
    score = compute_quality_score(width=1280, height=720, hdr=None, codec="h264", bitrate_kbps=3000)
    assert 25 <= score <= 40


def test_dolby_vision_beats_hdr10():
    score_dv = compute_quality_score(width=3840, height=2160, hdr="DOVI", codec="hevc", bitrate_kbps=40000)
    score_hdr10 = compute_quality_score(width=3840, height=2160, hdr="HDR10", codec="hevc", bitrate_kbps=40000)
    assert score_dv > score_hdr10


def test_av1_beats_hevc_same_res():
    score_av1 = compute_quality_score(width=1920, height=1080, hdr=None, codec="av1", bitrate_kbps=8000)
    score_hevc = compute_quality_score(width=1920, height=1080, hdr=None, codec="hevc", bitrate_kbps=8000)
    assert score_av1 > score_hevc


def test_parse_ffprobe_output_h264_1080p():
    ffprobe_json = json.dumps({
        "streams": [{
            "codec_type": "video",
            "codec_name": "h264",
            "width": 1920,
            "height": 1080,
            "bit_rate": "8000000",
            "color_transfer": "bt709",
            "side_data_list": [],
        }]
    })
    result = parse_ffprobe_output(ffprobe_json)
    assert result is not None
    assert result["codec"] == "h264"
    assert result["width"] == 1920
    assert result["hdr"] is None


def test_parse_ffprobe_output_no_video_stream():
    ffprobe_json = json.dumps({"streams": [{"codec_type": "audio"}]})
    assert parse_ffprobe_output(ffprobe_json) is None

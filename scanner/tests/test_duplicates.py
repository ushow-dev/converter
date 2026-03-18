# scanner/tests/test_duplicates.py
from scanner.services.duplicates import decide_action, UPGRADE_THRESHOLD


def test_no_existing_movie_register():
    assert decide_action(existing_score=None, new_score=50, ffprobe_ok=True) == "register"


def test_upgrade_candidate_above_threshold():
    assert decide_action(existing_score=40, new_score=50, ffprobe_ok=True) == "register"


def test_duplicate_below_threshold():
    assert decide_action(existing_score=45, new_score=50, ffprobe_ok=True) == "review_duplicate"


def test_duplicate_exact_threshold_is_upgrade():
    assert decide_action(existing_score=40, new_score=48, ffprobe_ok=True) == "register"


def test_unknown_quality_when_ffprobe_fails_with_existing():
    assert decide_action(existing_score=40, new_score=None, ffprobe_ok=False) == "review_unknown_quality"


def test_register_when_ffprobe_fails_no_existing():
    assert decide_action(existing_score=None, new_score=None, ffprobe_ok=False) == "register"


def test_upgrade_threshold_constant():
    assert UPGRADE_THRESHOLD == 8

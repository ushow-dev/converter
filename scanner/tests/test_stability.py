# scanner/tests/test_stability.py
from datetime import datetime, timedelta, timezone
from scanner.services.stability import is_stable, update_stability

UTC = timezone.utc


def _now():
    return datetime.now(UTC)


def test_new_file_is_not_stable():
    now = _now()
    assert not is_stable(
        current_size=1000,
        last_seen_size=None,
        stable_since=None,
        now=now,
        stability_sec=30,
    )


def test_size_changed_resets_stability():
    now = _now()
    stable_since = now - timedelta(seconds=60)
    assert not is_stable(
        current_size=2000,
        last_seen_size=1000,
        stable_since=stable_since,
        now=now,
        stability_sec=30,
    )


def test_stable_after_threshold():
    now = _now()
    stable_since = now - timedelta(seconds=31)
    assert is_stable(
        current_size=1000,
        last_seen_size=1000,
        stable_since=stable_since,
        now=now,
        stability_sec=30,
    )


def test_not_stable_before_threshold():
    now = _now()
    stable_since = now - timedelta(seconds=10)
    assert not is_stable(
        current_size=1000,
        last_seen_size=1000,
        stable_since=stable_since,
        now=now,
        stability_sec=30,
    )


def test_update_stability_size_changed_clears_stable_since():
    now = _now()
    result = update_stability(
        current_size=2000,
        last_seen_size=1000,
        stable_since=now - timedelta(seconds=60),
        now=now,
    )
    assert result["stable_since"] is None
    assert result["file_size_bytes"] == 2000


def test_update_stability_size_same_sets_stable_since():
    now = _now()
    result = update_stability(
        current_size=1000,
        last_seen_size=1000,
        stable_since=None,
        now=now,
    )
    assert result["stable_since"] == now
    assert result["file_size_bytes"] == 1000


def test_update_stability_keeps_existing_stable_since():
    now = _now()
    old_stable = now - timedelta(seconds=60)
    result = update_stability(
        current_size=1000,
        last_seen_size=1000,
        stable_since=old_stable,
        now=now,
    )
    assert result["stable_since"] == old_stable

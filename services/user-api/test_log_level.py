import logging

import pytest

from app import _parse_log_level


# `_parse_log_level` の境界条件を網羅する。
#
# 生値をそのまま `logging.basicConfig(level=...)` に渡すと Python 標準
# `logging._checkLevel` が `_nameToLevel` の大文字完全一致しか受理せず、
# `LOG_LEVEL=info` (小文字) / `LOG_LEVEL=` (空) / `LOG_LEVEL=verbose` (typo)
# 等で `ValueError` を投げてモジュール import が失敗する。
# 本テストは `analytics-engine` の `TestParseLogLevel` および
# `notification-service` の `parseLogLevel` テストと粒度を揃え、
# 3 サービス間で LOG_LEVEL 解釈が一貫していることを担保する。
@pytest.mark.parametrize(
    "raw, expected",
    [
        # None / 空 / 空白のみは INFO にフォールバックする
        (None, "INFO"),
        ("", "INFO"),
        ("   ", "INFO"),
        # 大文字はそのまま受理される
        ("DEBUG", "DEBUG"),
        ("INFO", "INFO"),
        ("WARN", "WARN"),
        ("WARNING", "WARNING"),
        ("ERROR", "ERROR"),
        ("CRITICAL", "CRITICAL"),
        ("FATAL", "FATAL"),
        ("NOTSET", "NOTSET"),
        # 小文字・混在ケースも大文字化して受理される
        ("debug", "DEBUG"),
        ("info", "INFO"),
        ("Warning", "WARNING"),
        ("eRRoR", "ERROR"),
        # 前後空白は strip される
        ("  DEBUG  ", "DEBUG"),
        ("\tinfo\n", "INFO"),
        # `logging._nameToLevel` に無い値はすべて INFO へ fail-safe に寄せる
        ("verbose", "INFO"),
        ("trace", "INFO"),
        ("SILENT", "INFO"),
        ("DEBU", "INFO"),         # 部分一致は不可
        ("INFO_EXTRA", "INFO"),   # 前方一致でもマッチさせない
    ],
)
def test_parse_log_level_normalizes_and_falls_back(raw, expected):
    assert _parse_log_level(raw) == expected


# `logging.basicConfig(level=_parse_log_level(...))` が本バグの再現ケース
# (小文字 / 空 / 空白 / 未知値) で `ValueError` を送出しないことを担保する。
# `_parse_log_level` の戻り値を Python 標準 `logging._checkLevel` に通す
# ことで、パーサ側と `logging` モジュール側の整合を回帰テストする。
@pytest.mark.parametrize(
    "raw",
    [None, "", "   ", "info", "  DEBUG  ", "verbose", "trace", "SILENT"],
)
def test_parse_log_level_output_is_accepted_by_logging(raw):
    parsed = _parse_log_level(raw)
    # `_checkLevel` は不正値で ValueError を投げる。ここを通れば
    # basicConfig(level=parsed) も import 時にクラッシュしない。
    numeric = logging._checkLevel(parsed)
    assert isinstance(numeric, int)
    assert numeric >= 0

from common.parser_config_utils import is_pdf_deepdoc_parse


def test_is_pdf_deepdoc_parse_true_for_default_deepdoc():
    assert is_pdf_deepdoc_parse("demo.pdf", {"layout_recognize": "DeepDOC"}) is True


def test_is_pdf_deepdoc_parse_true_for_boolean_flag():
    assert is_pdf_deepdoc_parse("demo.pdf", {"layout_recognize": True}) is True


def test_is_pdf_deepdoc_parse_false_for_plain_text():
    assert is_pdf_deepdoc_parse("demo.pdf", {"layout_recognize": "Plain Text"}) is False


def test_is_pdf_deepdoc_parse_false_for_other_pdf_parsers():
    assert is_pdf_deepdoc_parse("demo.pdf", {"layout_recognize": "MinerU"}) is False
    assert is_pdf_deepdoc_parse("demo.pdf", {"layout_recognize": "model-a@MinerU"}) is False
    assert is_pdf_deepdoc_parse("demo.pdf", {"layout_recognize": "PaddleOCR"}) is False
    assert is_pdf_deepdoc_parse("demo.pdf", {"layout_recognize": "TCADP Parser"}) is False


def test_is_pdf_deepdoc_parse_false_for_non_pdf():
    assert is_pdf_deepdoc_parse("demo.docx", {"layout_recognize": "DeepDOC"}) is False

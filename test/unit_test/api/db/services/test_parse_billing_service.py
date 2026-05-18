from api.db.services.billing_service import ParseBillingService


def test_quote_parse_returns_zero_for_non_deepdoc_pdf(monkeypatch):
    called = {"pages": 0}

    def fake_total_page_number(_filename, _blob):
        called["pages"] += 1
        return 8

    monkeypatch.setattr("api.db.services.billing_service.PdfParser.total_page_number", fake_total_page_number)

    quotes = ParseBillingService.quote_parse(
        filename="demo.pdf",
        blob=b"%PDF-1.4",
        parser_config={"layout_recognize": "Plain Text"},
    )

    assert quotes == []
    assert called["pages"] == 0


def test_quote_parse_returns_points_for_deepdoc_pdf(monkeypatch):
    monkeypatch.setattr("api.db.services.billing_service.PdfParser.total_page_number", lambda *_args, **_kwargs: 8)
    monkeypatch.setattr(
        "api.db.services.billing_service.PricePointService.get_by_name",
        lambda _name: {"consuming_point_amount": 3},
    )

    quotes = ParseBillingService.quote_parse(
        filename="demo.pdf",
        blob=b"%PDF-1.4",
        parser_config={"layout_recognize": "DeepDOC"},
    )

    assert len(quotes) == 1
    assert quotes[0].units == 8
    assert quotes[0].points == 24
    assert quotes[0].page_range == "1-8"


def test_hold_for_parse_skips_non_deepdoc_pdf(monkeypatch):
    hold_called = {"count": 0}

    monkeypatch.setattr("api.db.services.billing_service.PdfParser.total_page_number", lambda *_args, **_kwargs: 8)

    def fake_hold(**_kwargs):
        hold_called["count"] += 1
        return {"id": "hold-1"}

    monkeypatch.setattr("api.db.services.billing_service.PointAccountService.hold", fake_hold)

    hold = ParseBillingService.hold_for_parse(
        tenant_id="tenant-1",
        doc_id="doc-1",
        filename="demo.pdf",
        blob=b"%PDF-1.4",
        parser_config={"layout_recognize": "Plain Text"},
    )

    assert hold is None
    assert hold_called["count"] == 0


def test_hold_for_parse_treats_deepdoc_url_path_as_deepdoc(monkeypatch):
    monkeypatch.setattr("api.db.services.billing_service.PdfParser.total_page_number", lambda *_args, **_kwargs: 5)
    monkeypatch.setattr(
        "api.db.services.billing_service.PricePointService.get_by_name",
        lambda _name: {"consuming_point_amount": 2},
    )

    captured = {}

    def fake_hold(**kwargs):
        captured.update(kwargs)
        return {"id": "hold-1"}

    monkeypatch.setattr("api.db.services.billing_service.PointAccountService.hold", fake_hold)

    hold = ParseBillingService.hold_for_parse(
        tenant_id="tenant-1",
        doc_id="doc-1",
        filename="demo.pdf",
        blob=b"%PDF-1.4",
        parser_config={"layout_recognize": "DeepDOC"},
    )

    assert hold == {"id": "hold-1"}
    assert captured["points"] == 10

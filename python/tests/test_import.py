def test_import_and_version():
    import opentile_go
    assert isinstance(opentile_go.__version__, str)
    assert opentile_go.__version__

r"""Shared helpers for UI tests.

Reads the live app's embedded web-server address + token from
%APPDATA%\easytier-pro-gui\web.info so tests work regardless of the
random port the app binds on each launch.
"""
import json
import os
import sys

APP_NAME = "easytier-pro-gui"


def web_info_path():
    base = os.environ.get("APPDATA")
    if not base:
        base = os.path.expanduser("~")
    return os.path.join(base, APP_NAME, "web.info")


def load_web_info():
    path = web_info_path()
    if not os.path.exists(path):
        sys.exit("web.info not found at %s - is the app running?" % path)
    with open(path, "r", encoding="utf-8") as f:
        data = json.load(f)
    return data


def base_url():
    info = load_web_info()
    return "http://%s" % info["addr"]


def api_token():
    return load_web_info()["token"]

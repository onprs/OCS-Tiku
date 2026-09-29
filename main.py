import sys
import webbrowser
import threading
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import uvicorn
from config import config
from server import app

HOST = config.server_host
PORT = config.server_port


def open_browser():
    webbrowser.open(f"http://{HOST}:{PORT}")


if __name__ == "__main__":
    print(f"""
╔══════════════════════════════════════════╗
║     OCS 超星题库 Server v1.0             ║
║                                          ║
║     地址: http://{HOST}:{PORT}          ║
║     Dashboard: http://{HOST}:{PORT}      ║
║     按 Ctrl+C 停止                       ║
╚══════════════════════════════════════════╝
""")
    threading.Timer(1.0, open_browser).start()
    uvicorn.run(app, host=HOST, port=PORT, log_level="info")

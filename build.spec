# -*- mode: python ; coding: utf-8 -*-
import sys
import os

_PROJDIR = os.path.abspath(SPECPATH)

sys.path.insert(0, _PROJDIR)

a = Analysis(
    [os.path.join(_PROJDIR, 'main.py')],
    pathex=[_PROJDIR],
    binaries=[],
    datas=[
        (os.path.join(_PROJDIR, 'frontend', 'dist'), os.path.join('frontend', 'dist')),
    ],
    hiddenimports=[
        'aiosqlite',
        'httpx',
        'openai',
        'yaml',
        'PIL',
        'uvicorn.logging',
        'uvicorn.loops',
        'uvicorn.loops.auto',
        'uvicorn.protocols',
        'uvicorn.protocols.http',
        'uvicorn.protocols.http.auto',
        'uvicorn.protocols.websockets',
        'uvicorn.protocols.websockets.auto',
        'uvicorn.lifespan',
        'uvicorn.lifespan.on',
        'fastapi',
        'config',
        'db',
        'store',
        'solver',
        'preprocessor',
        'dashboard',
        'models',
        'models.base',
        'models.registry',
        'models.text',
        'models.text.deepseek',
        'models.text.openai_compat',
        'models.vision',
        'models.vision.openai_vl',
    ],
    hookspath=[],
    hooksconfig={},
    runtime_hooks=[],
    excludes=['jinja2'],
    noarchive=False,
    optimize=0,
)
pyz = PYZ(a.pure)

exe = EXE(
    pyz,
    a.scripts,
    a.binaries,
    a.datas,
    [],
    name='ocs-tiku',
    debug=False,
    bootloader_ignore_signals=False,
    strip=False,
    upx=True,
    upx_exclude=[],
    runtime_tmpdir=None,
    console=True,
    disable_windowed_traceback=False,
    argv_emulation=False,
    target_arch=None,
    codesign_identity=None,
    entitlements_file=None,
    icon=os.path.join(_PROJDIR, 'images', 'icon.ico') if os.path.exists(os.path.join(_PROJDIR, 'images', 'icon.ico')) else None,
)

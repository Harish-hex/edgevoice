#!/usr/bin/env python3
"""Offline tool: make a Piper voice loadable by sherpa-onnx (writes tokens.txt + adds ONNX metadata).

Usage: tools/.venv/bin/python tools/convert_piper.py models/piper-ta/ta_IN-ValluvarNeural-medium.onnx
Never imported by the runtime.
"""
import json
import sys

import onnx


def main(model_path: str) -> None:
    cfg = json.load(open(model_path + ".json"))
    out_dir = model_path.rsplit("/", 1)[0]
    with open(f"{out_dir}/tokens.txt", "w", encoding="utf-8") as f:
        for sym, ids in cfg["phoneme_id_map"].items():
            f.write(f"{sym} {ids[0]}\n")
    meta = {
        "model_type": "vits",
        "comment": "piper",
        "language": cfg["language"]["code"],
        "voice": cfg["espeak"]["voice"],
        "has_espeak": 1,
        "n_speakers": cfg["num_speakers"],
        "sample_rate": cfg["audio"]["sample_rate"],
    }
    m = onnx.load(model_path)
    del m.metadata_props[:]
    for k, v in meta.items():
        p = m.metadata_props.add()
        p.key, p.value = k, str(v)
    onnx.save(m, model_path)
    print("ok", meta)


if __name__ == "__main__":
    main(sys.argv[1])

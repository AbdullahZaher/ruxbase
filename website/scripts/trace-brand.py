"""Generate review-only flat vector candidates from the immutable source PNGs."""
import hashlib
import json
import pathlib
import subprocess
import tempfile
import xml.etree.ElementTree as ET
from PIL import Image, ImageChops, ImageOps, ImageDraw

root = pathlib.Path(__file__).resolve().parents[2]
brand = root / "assets/brand"
out = root / "website/brand-evidence"
out.mkdir(exist_ok=True)
manifest = json.loads((brand / "manifest.json").read_text())
ns = "{http://www.w3.org/2000/svg}"
report = {"status": "review-candidate", "method": "alpha >= 128 and luminance >= 128; flat silhouette of bright face, removes texture and black outline", "potrace": subprocess.check_output(["potrace", "--version"], text=True).splitlines()[0], "assets": []}
sheet = Image.new("RGB", (1100, 960), "#eceff1")
draw = ImageDraw.Draw(sheet)
with tempfile.TemporaryDirectory() as td:
    temp = pathlib.Path(td)
    for index, spec in enumerate(manifest["originals"]):
        src = brand / spec["file"]
        data = src.read_bytes()
        assert hashlib.sha1(b"blob " + str(len(data)).encode() + b"\0" + data).hexdigest() == spec["gitBlob"]
        im = Image.open(src).convert("RGBA")
        assert im.size == (spec["width"], spec["height"])
        mask = ImageChops.multiply(im.convert("L").point(lambda x: 255 if x >= 128 else 0), im.getchannel("A").point(lambda x: 255 if x >= 128 else 0))
        bbox = mask.getbbox()
        assert bbox
        crop = mask.crop(bbox)
        pad = max(2, round(max(crop.size) * 0.035))
        crop = ImageOps.expand(crop, border=pad, fill=0)
        pbm = temp / "mask.pbm"
        ImageOps.invert(crop).convert("1").save(pbm)
        svg = temp / "trace.svg"
        subprocess.run(["potrace", "--svg", "--turdsize", "10", "--alphamax", "1", "--opttolerance", "0.15", "-o", str(svg), str(pbm)], check=True)
        doc = ET.parse(svg).getroot()
        group = doc.find(ns + "g")
        assert group is not None
        paths = list(group.findall(ns + "path"))
        assert paths and len(paths) <= 200
        transform = group.attrib["transform"]
        role = spec["role"]
        outputs = {}
        for name, color in [("on-light", "#09090b"), ("on-dark", "#fafafa")]:
            element = ET.Element("svg", {"xmlns": "http://www.w3.org/2000/svg", "viewBox": doc.attrib["viewBox"], "role": "img"})
            ET.SubElement(element, "title").text = "Ruxbase " + role
            g = ET.SubElement(element, "g", {"transform": transform, "fill": color, "stroke": "none"})
            for path in paths:
                ET.SubElement(g, "path", {"d": path.attrib["d"]})
            dest = out / f"{role}-{name}.svg"
            dest.write_text(ET.tostring(element, encoding="unicode") + "\n")
            outputs[dest.name] = hashlib.sha256(dest.read_bytes()).hexdigest()
        # Compare the original bright-face raster with a rendered vector silhouette.
        raster = temp / "render.png"
        subprocess.run(["potrace", "--pgm", "--turdsize", "10", "--alphamax", "1", "--opttolerance", "0.15", "-o", str(temp / "trace.pgm"), str(pbm)], check=True)
        traced = ImageOps.invert(Image.open(temp / "trace.pgm").convert("L")).point(lambda x:255 if x>=128 else 0)
        if traced.size != crop.size:
            traced = traced.resize(crop.size, Image.Resampling.NEAREST)
        a = crop.histogram()[255]
        b = traced.histogram()[255]
        inter = ImageChops.multiply(crop, traced).histogram()[255]
        iou = inter / (a+b-inter)
        assert iou >= 0.97, (role, iou)
        report["assets"].append({"role": role, "source": spec["file"], "sourceGitBlob": spec["gitBlob"], "sourceSha256": hashlib.sha256(data).hexdigest(), "bbox": bbox, "paths": len(paths), "maskIoU": round(iou,6), "outputs": outputs})
        y = index * 270
        draw.text((20,y+12), role + " | source / dark face / light face", fill="#09090b")
        for col, (img, bg) in enumerate([(im,"#18181b"),(ImageOps.colorize(traced,"#ffffff","#09090b").convert("RGBA"),"#ffffff"),(ImageOps.colorize(traced,"#18181b","#fafafa").convert("RGBA"),"#18181b")]):
            panel=Image.new("RGBA",(350,225),bg)
            img.thumbnail((330,205),Image.Resampling.LANCZOS)
            panel.alpha_composite(img,((350-img.width)//2,(225-img.height)//2))
            sheet.paste(panel.convert("RGB"),(col*365,y+35))
        if role == "symbol":
            for col, size in enumerate([16,24,32,48,64]):
                icon=traced.resize((size,size), Image.Resampling.LANCZOS)
                swatch=ImageOps.colorize(icon,"#ffffff","#09090b")
                sheet.paste(swatch,(30+col*125,860))
                draw.text((30+col*125,930),str(size)+" px",fill="#09090b")
sheet.save(out / "comparison.png")
(out / "report.json").write_text(json.dumps(report,indent=2)+"\n")
print("Brand review candidates:", [(a["role"],a["maskIoU"],a["paths"]) for a in report["assets"]])

fn main() {
    println!("cargo:rerun-if-changed=model/common.onnx");
    println!("cargo:rerun-if-changed=model/common.json");
    for name in ["model/common.onnx", "model/common.json"] {
        if !std::path::Path::new(name).is_file() {
            panic!("missing {name}. GitHub Actions downloads the ddddocr model before cargo build");
        }
    }
}

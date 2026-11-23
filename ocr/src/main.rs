use std::io::{BufRead, Write};

use anyhow::{Context, Result, bail};
use base64::Engine;
use ddddocr::{CharsetRange, Ddddocr};
use serde::Deserialize;

const MAX_IMAGE: usize = 2 << 20;

#[derive(Deserialize)]
struct Job {
    image: String,
}

fn main() -> Result<()> {
    let stdout = std::io::stdout();
    let mut out = std::io::BufWriter::new(stdout.lock());
    let charset: ddddocr::Charset = include_str!("../model/common.json")
        .parse()
        .context("parse charset")?;
    let mut ocr = Ddddocr::new(include_bytes!("../model/common.onnx"), charset)
        .context("load ocr model")?;
    ocr.set_ranges(CharsetRange::LowercaseUppercaseDigit);
    emit(&mut out, serde_json::json!({"ready": true}))?;

    let stdin = std::io::stdin();
    for line in stdin.lock().lines() {
        let line = line.context("read job")?;
        if line.trim().is_empty() {
            continue;
        }
        let reply = match solve(&ocr, &line) {
            Ok(text) => serde_json::json!({"text": text}),
            Err(err) => serde_json::json!({"error": one_line(&err)}),
        };
        emit(&mut out, reply)?;
    }
    Ok(())
}

fn emit(out: &mut impl Write, value: serde_json::Value) -> Result<()> {
    serde_json::to_writer(&mut *out, &value)?;
    out.write_all(b"\n")?;
    out.flush()?;
    Ok(())
}

fn one_line(err: &anyhow::Error) -> String {
    err.to_string().replace(['\n', '\r'], " ")
}

fn solve(ocr: &Ddddocr<'_>, line: &str) -> Result<String> {
    let job: Job = serde_json::from_str(line).context("parse job")?;
    let bytes = base64::engine::general_purpose::STANDARD
        .decode(job.image.trim())
        .context("decode image")?;
    if bytes.is_empty() || bytes.len() > MAX_IMAGE {
        bail!("验证码图片大小异常");
    }
    let prob = ocr.classification_probability(&bytes).context("ocr")?;
    let text = decode_text(&prob);
    if text.is_empty() {
        bail!("识别结果为空");
    }
    Ok(text)
}

fn decode_text(prob: &ddddocr::CharacterProbability) -> String {
    let mut out = String::new();
    let mut prev = String::new();
    for step in &prob.probability {
        let Some((idx, _)) = step
            .iter()
            .enumerate()
            .max_by(|(_, left), (_, right)| left.total_cmp(right))
        else {
            continue;
        };
        let ch = prob.charset.get(idx).map(String::as_str).unwrap_or("");
        if ch.is_empty() {
            prev.clear();
            continue;
        }
        if ch == prev {
            continue;
        }
        prev = ch.to_string();
        if ch.chars().all(|item| item.is_ascii_alphanumeric()) {
            out.push_str(ch);
        }
        if out.len() > 16 {
            break;
        }
    }
    out
}

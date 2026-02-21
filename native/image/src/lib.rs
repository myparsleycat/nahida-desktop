#![deny(clippy::all)]

use std::fs::File;
use std::io::{BufWriter, Cursor, Write};
use std::path::Path;

use image::codecs::avif::AvifEncoder;
use image::codecs::jpeg::JpegEncoder;
use image::codecs::png::PngEncoder;
use image::codecs::webp::WebPEncoder;
use image::imageops::FilterType;
use image::{DynamicImage, GenericImageView, ImageEncoder, ImageFormat, ImageReader};
use napi::bindgen_prelude::*;
use napi_derive::napi;

#[napi(object)]
#[derive(Clone)]
pub struct ResizeOptions {
    pub width: u32,
    pub height: u32,
    /// png, jpeg, webp, avif
    pub format: String,
    pub quality: Option<u32>,
    pub save_path: Option<String>,
}

#[napi]
pub async fn convert_image(input_path: String, options: ResizeOptions) -> Result<Option<Vec<u8>>> {
    tokio::task::spawn_blocking(move || -> Result<Option<Vec<u8>>> {
        let path_str = input_path.clone();
        let path = Path::new(&path_str);

        let reader = ImageReader::open(path)
            .map_err(|e| Error::from_reason(format!("Failed to open file: {}", e)))?
            .with_guessed_format()
            .map_err(|e| Error::from_reason(format!("Failed to guess format: {}", e)))?;

        let target_format = parse_format(&options.format);

        let img = reader
            .decode()
            .map_err(|e| Error::from_reason(format!("Decode failed: {}", e)))?;

        let resized = img.resize(options.width, options.height, FilterType::Lanczos3);

        handle_output(&resized, target_format, &options)
    })
    .await
    .map_err(|e| Error::from_reason(format!("Thread join error: {}", e)))?
}

fn handle_output(
    img: &DynamicImage,
    format: ImageFormat,
    options: &ResizeOptions,
) -> Result<Option<Vec<u8>>> {
    let mut buffer = Vec::new();
    let mut cursor = Cursor::new(&mut buffer);
    let (w, h) = img.dimensions();
    let quality = options.quality.unwrap_or(80).min(100) as u8;

    match format {
        ImageFormat::Jpeg => {
            let rgb_img = img.to_rgb8();
            let encoder = JpegEncoder::new_with_quality(&mut cursor, quality);
            encoder
                .write_image(rgb_img.as_raw(), w, h, image::ColorType::Rgb8.into())
                .map_err(|e| Error::from_reason(e.to_string()))?;
        }
        ImageFormat::Png => {
            let encoder = PngEncoder::new(&mut cursor);
            img.write_with_encoder(encoder)
                .map_err(|e| Error::from_reason(e.to_string()))?;
        }
        ImageFormat::WebP => {
            let encoder = WebPEncoder::new_lossless(&mut cursor);
            img.write_with_encoder(encoder)
                .map_err(|e| Error::from_reason(e.to_string()))?;
        }
        ImageFormat::Avif => {
            let encoder = AvifEncoder::new(&mut cursor);
            img.write_with_encoder(encoder)
                .map_err(|e| Error::from_reason(e.to_string()))?;
        }
        _ => {
            img.write_to(&mut cursor, format)
                .map_err(|e| Error::from_reason(format!("Encode failed: {}", e)))?;
        }
    }

    finalize_output(buffer, options)
}

fn finalize_output(data: Vec<u8>, options: &ResizeOptions) -> Result<Option<Vec<u8>>> {
    if let Some(path_str) = &options.save_path {
        let path = Path::new(path_str);
        let file = File::create(path)
            .map_err(|e| Error::from_reason(format!("File create error: {}", e)))?;
        let mut writer = BufWriter::new(file);
        writer
            .write_all(&data)
            .map_err(|e| Error::from_reason(format!("File write error: {}", e)))?;
        Ok(None)
    } else {
        Ok(Some(data))
    }
}

fn parse_format(fmt: &str) -> ImageFormat {
    match fmt.to_lowercase().as_str() {
        "jpg" | "jpeg" => ImageFormat::Jpeg,
        "png" => ImageFormat::Png,
        "webp" => ImageFormat::WebP,
        "avif" => ImageFormat::Avif,
        _ => ImageFormat::Png,
    }
}

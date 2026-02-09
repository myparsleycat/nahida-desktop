#![deny(clippy::all)]

use std::fs::File;
use std::io::{BufReader, BufWriter, Cursor, Write};
use std::path::Path;

use image::codecs::avif::AvifEncoder;
use image::codecs::gif::{GifDecoder, GifEncoder, Repeat};
use image::codecs::jpeg::JpegEncoder;
use image::codecs::png::PngEncoder;
use image::codecs::webp::WebPEncoder;
use image::imageops::FilterType;
use image::{
    AnimationDecoder, DynamicImage, Frame, GenericImageView, ImageEncoder, ImageFormat,
    ImageReader, ImageResult,
};
use napi::bindgen_prelude::*;
use napi_derive::napi;

#[napi(object)]
pub struct ResizeOptions {
    pub width: u32,
    pub height: u32,
    /// png, jpeg, webp, gif, avif
    pub format: String,
    pub quality: Option<u32>,
    pub save_path: Option<String>,
}

#[napi]
pub fn convert_image(input_path: String, options: ResizeOptions) -> Result<Option<Buffer>> {
    let path = Path::new(&input_path);

    let reader = ImageReader::open(path)
        .map_err(|e| Error::from_reason(format!("Failed to open file: {}", e)))?
        .with_guessed_format()
        .map_err(|e| Error::from_reason(format!("Failed to guess format: {}", e)))?;

    let input_format = reader.format().unwrap_or(ImageFormat::Png);
    let target_format = parse_format(&options.format);

    if input_format == ImageFormat::Gif && target_format == ImageFormat::Gif {
        return process_animated_gif(path, &options);
    }

    let img = reader
        .decode()
        .map_err(|e| Error::from_reason(format!("Decode failed: {}", e)))?;

    let resized = img.resize(options.width, options.height, FilterType::Lanczos3);

    handle_output(&resized, target_format, &options)
}

fn process_animated_gif(path: &Path, options: &ResizeOptions) -> Result<Option<Buffer>> {
    let file = File::open(path).map_err(|e| Error::from_reason(e.to_string()))?;
    let reader = BufReader::new(file);
    let decoder = GifDecoder::new(reader).map_err(|e| Error::from_reason(e.to_string()))?;

    let frames = decoder.into_frames();
    let frames: Vec<Frame> = frames
        .collect::<ImageResult<Vec<Frame>>>()
        .map_err(|e| Error::from_reason(e.to_string()))?;

    let mut output_buffer = Vec::new();
    {
        let mut encoder = GifEncoder::new(&mut output_buffer);

        encoder
            .set_repeat(Repeat::Infinite)
            .map_err(|e| Error::from_reason(e.to_string()))?;

        for frame in frames {
            let delay = frame.delay();

            let frame_img = DynamicImage::ImageRgba8(frame.into_buffer());

            let resized_frame_img =
                frame_img.resize(options.width, options.height, FilterType::Lanczos3);

            let new_frame = Frame::from_parts(resized_frame_img.into_rgba8(), 0, 0, delay);

            encoder
                .encode_frame(new_frame)
                .map_err(|e| Error::from_reason(format!("GIF encode error: {}", e)))?;
        }
    }

    finalize_output(output_buffer, options)
}

fn handle_output(
    img: &DynamicImage,
    format: ImageFormat,
    options: &ResizeOptions,
) -> Result<Option<Buffer>> {
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

fn finalize_output(data: Vec<u8>, options: &ResizeOptions) -> Result<Option<Buffer>> {
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
        Ok(Some(Buffer::from(data)))
    }
}

fn parse_format(fmt: &str) -> ImageFormat {
    match fmt.to_lowercase().as_str() {
        "jpg" | "jpeg" => ImageFormat::Jpeg,
        "png" => ImageFormat::Png,
        "gif" => ImageFormat::Gif,
        "webp" => ImageFormat::WebP,
        "avif" => ImageFormat::Avif,
        _ => ImageFormat::Png,
    }
}

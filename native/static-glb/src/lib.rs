#![deny(clippy::all)]

use ddsfile::Dds;
use image::codecs::jpeg::JpegEncoder;
use image::codecs::png::PngEncoder;
use image::{ColorType, ImageEncoder, ImageReader, RgbaImage};
use napi::bindgen_prelude::{AsyncTask, Buffer};
use napi::Task;
use napi_derive::napi;
use sha2::{Digest, Sha256};
use std::io::Cursor;
use std::path::{Path, PathBuf};

#[napi(object)]
pub struct PngAnalysis {
    pub has_alpha: bool,
    pub low_ratio: f64,
    pub high_ratio: f64,
    pub partial_ratio: f64,
    pub low_alpha_rgb_mean: f64,
    pub channel_range_max: u32,
    pub luminance_std_dev: f64,
    pub mean_r: f64,
    pub mean_g: f64,
    pub mean_b: f64,
    pub blue_dominance: f64,
}

#[napi(object)]
pub struct DrawRange {
    pub index_count: i32,
    pub start_index: i32,
    pub base_vertex: i32,
}

#[napi(object)]
pub struct MergeDrawIndicesResult {
    pub indices: Buffer,
    pub invalid_ranges: Vec<String>,
}

#[napi(object)]
pub struct RemoveDegenerateTrianglesResult {
    pub indices: Buffer,
    pub removed: u32,
}

#[napi(object)]
pub struct PrepareTextureForMaterialInput {
    pub texture_path: String,
    pub resource_name: String,
    pub texture_format: String,
    pub jpeg_quality: u32,
    pub allow_cache_reuse: bool,
    pub cache_dir: String,
}

#[napi(object)]
pub struct PrepareTextureForMaterialResult {
    pub image: Option<Buffer>,
    pub image_path: Option<String>,
    pub mime_type: String,
    pub image_extension: String,
    pub uses_alpha: bool,
    pub inverted_alpha: bool,
    pub selection_score: i32,
    pub srgb_confidence: String,
    pub alpha_mode: Option<String>,
    pub alpha_cutoff: Option<f64>,
}

pub struct PrepareTextureForMaterialTask {
    input: PrepareTextureForMaterialInput,
}

#[napi]
pub fn analyze_png(data: Buffer, width: u32, height: u32) -> napi::Result<PngAnalysis> {
    analyze_rgba(&data, width, height)
}

#[napi(ts_return_type = "Promise<PrepareTextureForMaterialResult>")]
pub fn prepare_texture_for_material(
    input: PrepareTextureForMaterialInput,
) -> AsyncTask<PrepareTextureForMaterialTask> {
    AsyncTask::new(PrepareTextureForMaterialTask { input })
}

impl Task for PrepareTextureForMaterialTask {
    type Output = PrepareTextureForMaterialResult;
    type JsValue = PrepareTextureForMaterialResult;

    fn compute(&mut self) -> napi::Result<Self::Output> {
        let input = &self.input;
        let texture_path = Path::new(&input.texture_path);
        if input.allow_cache_reuse {
            if let Some(cached) = read_cached_prepared_texture(input, texture_path)? {
                return Ok(cached);
            }
        }

        let extension = texture_path
            .extension()
            .and_then(|value| value.to_str())
            .map(|value| value.to_ascii_lowercase())
            .unwrap_or_default();

        let (mut rgba, is_likely_srgb) = if extension == "dds" {
            load_dds_rgba(texture_path)?
        } else if extension == "png" {
            load_png_rgba(texture_path)?
        } else {
            return Err(napi::Error::from_reason(format!(
                "Unsupported texture type for GLB embedding: {}",
                input.texture_path
            )));
        };

        let analysis = analyze_rgba(rgba.as_raw(), rgba.width(), rgba.height())?;
        let resource_key = normalize_key(&input.resource_name);
        let texture_key = normalize_key(&format!(
            "{} {}",
            input.resource_name,
            texture_path
                .file_name()
                .and_then(|value| value.to_str())
                .unwrap_or_default()
        ));
        let srgb_confidence = classify_srgb_confidence(is_likely_srgb);

        let selection = analyze_texture_selection_native(
            &analysis,
            &resource_key,
            is_likely_srgb,
            srgb_confidence,
        );

        let mut final_analysis = analysis;
        let mut inverted_alpha = false;
        if final_analysis.has_alpha && should_invert_alpha(&texture_key, &final_analysis) {
            invert_rgba_alpha_in_place(rgba.as_mut());
            final_analysis = analysis_after_alpha_invert(&final_analysis);
            inverted_alpha = true;
        }

        let uses_alpha = texture_uses_alpha(&final_analysis);
        let mime_type = resolve_texture_mime_type(&input.texture_format, Some(uses_alpha))?;
        let (image, image_extension) =
            encode_prepared_image(&rgba, &mime_type, input.jpeg_quality)?;
        let score = score_texture_selection(&resource_key, &selection);
        let (alpha_mode, alpha_cutoff) = material_alpha_mode(&final_analysis);
        let mut result = PrepareTextureForMaterialResult {
            image: Some(image.into()),
            image_path: None,
            mime_type,
            image_extension,
            uses_alpha,
            inverted_alpha,
            selection_score: score,
            srgb_confidence: srgb_confidence.to_string(),
            alpha_mode,
            alpha_cutoff,
        };

        if let Some(cache_path) = write_cached_prepared_texture(input, texture_path, &result)? {
            result.image = None;
            result.image_path = Some(cache_path.to_string_lossy().into_owned());
        }

        Ok(result)
    }

    fn resolve(&mut self, _: napi::Env, output: Self::Output) -> napi::Result<Self::JsValue> {
        Ok(output)
    }
}

fn analyze_rgba(data: &[u8], width: u32, height: u32) -> napi::Result<PngAnalysis> {
    let pixel_count = width as usize * height as usize;
    let expected_len = pixel_count * 4;
    if data.len() < expected_len {
        return Err(napi::Error::from_reason(format!(
            "PNG buffer is shorter than expected RGBA data: got {}, expected at least {}",
            data.len(),
            expected_len
        )));
    }

    let mut low = 0usize;
    let mut high = 0usize;
    let mut partial = 0usize;
    let mut low_alpha_rgb_total = 0f64;

    let mut min_r = 255u8;
    let mut min_g = 255u8;
    let mut min_b = 255u8;
    let mut max_r = 0u8;
    let mut max_g = 0u8;
    let mut max_b = 0u8;
    let mut sample_count = 0usize;
    let mut sum_r = 0f64;
    let mut sum_g = 0f64;
    let mut sum_b = 0f64;
    let mut luminance_sum = 0f64;
    let mut luminance_square_sum = 0f64;

    for offset in (0..expected_len).step_by(4) {
        let alpha = data[offset + 3];
        if alpha <= 16 {
            low += 1;
            low_alpha_rgb_total +=
                (data[offset] as f64 + data[offset + 1] as f64 + data[offset + 2] as f64) / 3.0;
        } else if alpha >= 239 {
            high += 1;
        } else {
            partial += 1;
        }
    }

    let stride = (((width as usize * height as usize) as f64 / 4096.0)
        .sqrt()
        .floor() as usize)
        .max(1);

    for y in (0..height as usize).step_by(stride) {
        for x in (0..width as usize).step_by(stride) {
            let offset = (y * width as usize + x) * 4;
            let r = data[offset];
            let g = data[offset + 1];
            let b = data[offset + 2];
            min_r = min_r.min(r);
            min_g = min_g.min(g);
            min_b = min_b.min(b);
            max_r = max_r.max(r);
            max_g = max_g.max(g);
            max_b = max_b.max(b);
            sum_r += r as f64;
            sum_g += g as f64;
            sum_b += b as f64;
            let luminance = (0.2126 * r as f64 + 0.7152 * g as f64 + 0.0722 * b as f64) / 255.0;
            luminance_sum += luminance;
            luminance_square_sum += luminance * luminance;
            sample_count += 1;
        }
    }

    let mean = if sample_count > 0 {
        luminance_sum / sample_count as f64
    } else {
        0.0
    };
    let variance = if sample_count > 0 {
        (luminance_square_sum / sample_count as f64 - mean * mean).max(0.0)
    } else {
        0.0
    };
    let mean_r = if sample_count > 0 {
        sum_r / sample_count as f64 / 255.0
    } else {
        0.0
    };
    let mean_g = if sample_count > 0 {
        sum_g / sample_count as f64 / 255.0
    } else {
        0.0
    };
    let mean_b = if sample_count > 0 {
        sum_b / sample_count as f64 / 255.0
    } else {
        0.0
    };

    Ok(PngAnalysis {
        has_alpha: low > 0 || partial > 0,
        low_ratio: ratio(low, pixel_count),
        high_ratio: ratio(high, pixel_count),
        partial_ratio: ratio(partial, pixel_count),
        low_alpha_rgb_mean: if low > 0 {
            low_alpha_rgb_total / low as f64
        } else {
            0.0
        },
        channel_range_max: (max_r.saturating_sub(min_r) as u32)
            .max(max_g.saturating_sub(min_g) as u32)
            .max(max_b.saturating_sub(min_b) as u32),
        luminance_std_dev: variance.sqrt(),
        mean_r,
        mean_g,
        mean_b,
        blue_dominance: mean_b - mean_r.max(mean_g),
    })
}

#[napi]
pub fn invert_rgba_alpha(data: Buffer) -> Buffer {
    let mut out = data.to_vec();
    for offset in (3..out.len()).step_by(4) {
        out[offset] = 255u8.saturating_sub(out[offset]);
    }
    out.into()
}

#[napi]
pub fn parse_dds_srgb_state(bytes: Buffer) -> Option<bool> {
    if bytes.len() < 148 || &bytes[..4] != b"DDS " {
        return None;
    }

    let four_cc = std::str::from_utf8(&bytes[84..88]).ok()?;
    if four_cc != "DX10" {
        return None;
    }

    let dxgi_format = u32::from_le_bytes(bytes[128..132].try_into().ok()?);
    match dxgi_format {
        29 | 72 | 75 | 78 | 91 | 93 | 99 => Some(true),
        28 | 71 | 74 | 77 | 80 | 83 | 87 | 88 | 95 | 98 => Some(false),
        _ => None,
    }
}

fn load_dds_rgba(texture_path: &Path) -> napi::Result<(RgbaImage, Option<bool>)> {
    let bytes = std::fs::read(texture_path).map_err(|error| {
        napi::Error::from_reason(format!(
            "Failed to read DDS file '{}': {}",
            texture_path.display(),
            error
        ))
    })?;
    let srgb = parse_dds_srgb_state(bytes[..bytes.len().min(148)].to_vec().into());
    let mut reader = Cursor::new(&bytes);
    let dds = Dds::read(&mut reader).map_err(|error| {
        napi::Error::from_reason(format!(
            "Failed to read DDS file '{}': {}",
            texture_path.display(),
            error
        ))
    })?;
    let image = image_dds::image_from_dds(&dds, 0).map_err(|error| {
        napi::Error::from_reason(format!(
            "Failed to decode DDS file '{}': {}",
            texture_path.display(),
            error
        ))
    })?;
    Ok((image, srgb))
}

fn load_png_rgba(texture_path: &Path) -> napi::Result<(RgbaImage, Option<bool>)> {
    let image = ImageReader::open(texture_path)
        .map_err(|error| {
            napi::Error::from_reason(format!(
                "Failed to open PNG file '{}': {}",
                texture_path.display(),
                error
            ))
        })?
        .with_guessed_format()
        .map_err(|error| {
            napi::Error::from_reason(format!(
                "Failed to detect image format for '{}': {}",
                texture_path.display(),
                error
            ))
        })?
        .decode()
        .map_err(|error| {
            napi::Error::from_reason(format!(
                "Failed to decode PNG file '{}': {}",
                texture_path.display(),
                error
            ))
        })?;
    Ok((image.to_rgba8(), None))
}

fn read_cached_prepared_texture(
    input: &PrepareTextureForMaterialInput,
    texture_path: &Path,
) -> napi::Result<Option<PrepareTextureForMaterialResult>> {
    if input.cache_dir.is_empty() {
        return Ok(None);
    }

    for cache_path in prepared_texture_cache_candidates(input, texture_path) {
        let metadata_path = prepared_texture_metadata_path(&cache_path);
        if !is_cache_up_to_date(&cache_path, texture_path)?
            || !is_cache_up_to_date(&metadata_path, texture_path)?
        {
            continue;
        }

        let metadata = match read_prepared_texture_metadata(&metadata_path)? {
            Some(metadata) => metadata,
            None => continue,
        };

        return Ok(Some(PrepareTextureForMaterialResult {
            image: None,
            image_path: Some(cache_path.to_string_lossy().into_owned()),
            mime_type: metadata.mime_type,
            image_extension: metadata.image_extension,
            uses_alpha: metadata.uses_alpha,
            inverted_alpha: metadata.inverted_alpha,
            selection_score: metadata.selection_score,
            srgb_confidence: metadata.srgb_confidence,
            alpha_mode: metadata.alpha_mode,
            alpha_cutoff: metadata.alpha_cutoff,
        }));
    }

    Ok(None)
}

fn write_cached_prepared_texture(
    input: &PrepareTextureForMaterialInput,
    texture_path: &Path,
    result: &PrepareTextureForMaterialResult,
) -> napi::Result<Option<PathBuf>> {
    if input.cache_dir.is_empty() {
        return Ok(None);
    }

    let cache_path = prepared_texture_cache_path(
        &input.cache_dir,
        texture_path,
        &input.texture_path,
        &result.mime_type,
        &result.image_extension,
        input.jpeg_quality,
    );
    if let Some(parent) = cache_path.parent() {
        std::fs::create_dir_all(parent).map_err(|error| {
            napi::Error::from_reason(format!(
                "Failed to create texture cache directory '{}': {}",
                parent.display(),
                error
            ))
        })?;
    }

    let image = result.image.as_ref().ok_or_else(|| {
        napi::Error::from_reason(
            "Prepared texture bytes were missing while attempting to write cache".to_string(),
        )
    })?;

    std::fs::write(&cache_path, image).map_err(|error| {
        napi::Error::from_reason(format!(
            "Failed to write cached texture '{}': {}",
            cache_path.display(),
            error
        ))
    })?;

    let metadata = PreparedTextureMetadata {
        mime_type: result.mime_type.clone(),
        image_extension: result.image_extension.clone(),
        uses_alpha: result.uses_alpha,
        inverted_alpha: result.inverted_alpha,
        selection_score: result.selection_score,
        srgb_confidence: result.srgb_confidence.clone(),
        alpha_mode: result.alpha_mode.clone(),
        alpha_cutoff: result.alpha_cutoff,
    };
    let metadata_path = prepared_texture_metadata_path(&cache_path);
    std::fs::write(&metadata_path, metadata.serialize()).map_err(|error| {
        napi::Error::from_reason(format!(
            "Failed to write texture cache metadata '{}': {}",
            metadata_path.display(),
            error
        ))
    })?;

    Ok(Some(cache_path))
}

fn prepared_texture_cache_candidates(
    input: &PrepareTextureForMaterialInput,
    texture_path: &Path,
) -> Vec<PathBuf> {
    match input.texture_format.as_str() {
        "png" => vec![prepared_texture_cache_path(
            &input.cache_dir,
            texture_path,
            &input.texture_path,
            "image/png",
            "png",
            input.jpeg_quality,
        )],
        "jpeg-force" => vec![prepared_texture_cache_path(
            &input.cache_dir,
            texture_path,
            &input.texture_path,
            "image/jpeg",
            "jpg",
            input.jpeg_quality,
        )],
        "jpeg-safe" => vec![
            prepared_texture_cache_path(
                &input.cache_dir,
                texture_path,
                &input.texture_path,
                "image/png",
                "png",
                input.jpeg_quality,
            ),
            prepared_texture_cache_path(
                &input.cache_dir,
                texture_path,
                &input.texture_path,
                "image/jpeg",
                "jpg",
                input.jpeg_quality,
            ),
        ],
        _ => Vec::new(),
    }
}

fn prepared_texture_cache_path(
    cache_dir: &str,
    texture_path: &Path,
    resolved_texture_path: &str,
    mime_type: &str,
    image_extension: &str,
    jpeg_quality: u32,
) -> PathBuf {
    let file_name = if mime_type == "image/png" {
        format!(
            "{}-prepared.{}",
            create_texture_cache_base_name(texture_path, resolved_texture_path),
            image_extension
        )
    } else {
        format!(
            "{}-q{}.{}",
            create_texture_cache_base_name(texture_path, resolved_texture_path),
            jpeg_quality,
            image_extension
        )
    };
    Path::new(cache_dir).join(file_name)
}

fn prepared_texture_metadata_path(cache_path: &Path) -> PathBuf {
    let mut file_name = cache_path
        .file_name()
        .and_then(|value| value.to_str())
        .unwrap_or_default()
        .to_string();
    file_name.push_str(".meta");
    cache_path.with_file_name(file_name)
}

fn create_texture_cache_base_name(texture_path: &Path, resolved_texture_path: &str) -> String {
    let extensionless = texture_path
        .file_stem()
        .and_then(|value| value.to_str())
        .filter(|value| !value.is_empty())
        .unwrap_or("texture");
    let mut hasher = Sha256::new();
    hasher.update(resolved_texture_path.as_bytes());
    let digest = hasher.finalize();
    let digest_prefix: String = digest[..6]
        .iter()
        .map(|value| format!("{value:02x}"))
        .collect();
    format!("{extensionless}-{digest_prefix}")
}

fn is_cache_up_to_date(cache_path: &Path, source_path: &Path) -> napi::Result<bool> {
    let cache_metadata = match std::fs::metadata(cache_path) {
        Ok(metadata) => metadata,
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => return Ok(false),
        Err(error) => {
            return Err(napi::Error::from_reason(format!(
                "Failed to stat cache artifact '{}': {}",
                cache_path.display(),
                error
            )))
        }
    };

    let source_metadata = std::fs::metadata(source_path).map_err(|error| {
        napi::Error::from_reason(format!(
            "Failed to stat source texture '{}': {}",
            source_path.display(),
            error
        ))
    })?;

    let cache_mtime = cache_metadata.modified().map_err(|error| {
        napi::Error::from_reason(format!(
            "Failed to read cache mtime '{}': {}",
            cache_path.display(),
            error
        ))
    })?;
    let source_mtime = source_metadata.modified().map_err(|error| {
        napi::Error::from_reason(format!(
            "Failed to read source texture mtime '{}': {}",
            source_path.display(),
            error
        ))
    })?;

    Ok(cache_mtime >= source_mtime)
}

struct PreparedTextureMetadata {
    mime_type: String,
    image_extension: String,
    uses_alpha: bool,
    inverted_alpha: bool,
    selection_score: i32,
    srgb_confidence: String,
    alpha_mode: Option<String>,
    alpha_cutoff: Option<f64>,
}

impl PreparedTextureMetadata {
    fn serialize(&self) -> String {
        format!(
      "mime_type={}\nimage_extension={}\nuses_alpha={}\ninverted_alpha={}\nselection_score={}\nsrgb_confidence={}\nalpha_mode={}\nalpha_cutoff={}\n",
      self.mime_type,
      self.image_extension,
      self.uses_alpha,
      self.inverted_alpha,
      self.selection_score,
      self.srgb_confidence,
      self.alpha_mode.as_deref().unwrap_or_default(),
      self.alpha_cutoff.map(|value| value.to_string()).unwrap_or_default(),
    )
    }

    fn deserialize(value: &str) -> Option<Self> {
        let mut mime_type = None;
        let mut image_extension = None;
        let mut uses_alpha = None;
        let mut inverted_alpha = None;
        let mut selection_score = None;
        let mut srgb_confidence = None;
        let mut alpha_mode = None;
        let mut alpha_cutoff = None;

        for line in value.lines() {
            let (key, raw_value) = line.split_once('=')?;
            match key {
                "mime_type" => mime_type = Some(raw_value.to_string()),
                "image_extension" => image_extension = Some(raw_value.to_string()),
                "uses_alpha" => uses_alpha = raw_value.parse::<bool>().ok(),
                "inverted_alpha" => inverted_alpha = raw_value.parse::<bool>().ok(),
                "selection_score" => selection_score = raw_value.parse::<i32>().ok(),
                "srgb_confidence" => srgb_confidence = Some(raw_value.to_string()),
                "alpha_mode" => {
                    alpha_mode = if raw_value.is_empty() {
                        Some(None)
                    } else {
                        Some(Some(raw_value.to_string()))
                    }
                }
                "alpha_cutoff" => {
                    alpha_cutoff = if raw_value.is_empty() {
                        Some(None)
                    } else {
                        raw_value.parse::<f64>().ok().map(Some)
                    }
                }
                _ => {}
            }
        }

        Some(Self {
            mime_type: mime_type?,
            image_extension: image_extension?,
            uses_alpha: uses_alpha?,
            inverted_alpha: inverted_alpha?,
            selection_score: selection_score?,
            srgb_confidence: srgb_confidence?,
            alpha_mode: alpha_mode.unwrap_or(None),
            alpha_cutoff: alpha_cutoff.unwrap_or(None),
        })
    }
}

fn read_prepared_texture_metadata(
    metadata_path: &Path,
) -> napi::Result<Option<PreparedTextureMetadata>> {
    let contents = match std::fs::read_to_string(metadata_path) {
        Ok(contents) => contents,
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => return Ok(None),
        Err(error) => {
            return Err(napi::Error::from_reason(format!(
                "Failed to read texture cache metadata '{}': {}",
                metadata_path.display(),
                error
            )))
        }
    };

    Ok(PreparedTextureMetadata::deserialize(&contents))
}

fn encode_prepared_image(
    rgba: &RgbaImage,
    mime_type: &str,
    jpeg_quality: u32,
) -> napi::Result<(Vec<u8>, String)> {
    let mut output = Vec::new();
    if mime_type == "image/png" {
        let encoder = PngEncoder::new(&mut output);
        encoder
            .write_image(
                rgba.as_raw(),
                rgba.width(),
                rgba.height(),
                ColorType::Rgba8.into(),
            )
            .map_err(|error| {
                napi::Error::from_reason(format!("Failed to encode PNG texture: {error}"))
            })?;
        return Ok((output, String::from("png")));
    }

    if mime_type == "image/jpeg" {
        let mut encoder =
            JpegEncoder::new_with_quality(&mut output, jpeg_quality.clamp(1, 100) as u8);
        encoder.encode_image(rgba).map_err(|error| {
            napi::Error::from_reason(format!("Failed to encode JPEG texture: {error}"))
        })?;
        return Ok((output, String::from("jpg")));
    }

    Err(napi::Error::from_reason(format!(
        "Unsupported output mime type: {mime_type}"
    )))
}

fn resolve_texture_mime_type(format: &str, uses_alpha: Option<bool>) -> napi::Result<String> {
    match format {
        "png" => Ok(String::from("image/png")),
        "jpeg-force" => Ok(String::from("image/jpeg")),
        "jpeg-safe" => match uses_alpha {
            Some(true) => Ok(String::from("image/png")),
            Some(false) => Ok(String::from("image/jpeg")),
            None => Ok(String::from("image/png")),
        },
        _ => Err(napi::Error::from_reason(format!(
            "Unsupported texture format option: {format}"
        ))),
    }
}

fn material_alpha_mode(alpha: &PngAnalysis) -> (Option<String>, Option<f64>) {
    if is_cutout_alpha(alpha) {
        return (Some(String::from("MASK")), Some(0.5));
    }
    (None, None)
}

fn texture_uses_alpha(alpha: &PngAnalysis) -> bool {
    if is_cutout_alpha(alpha) {
        return true;
    }
    alpha.partial_ratio > 0.0 || alpha.low_ratio >= 0.005
}

fn is_cutout_alpha(alpha: &PngAnalysis) -> bool {
    alpha.low_ratio >= 0.005 && alpha.high_ratio >= 0.5 && alpha.partial_ratio <= 0.02
}

fn should_invert_alpha(texture_key: &str, alpha: &PngAnalysis) -> bool {
    if texture_key.contains("invertalpha") || texture_key.contains("alphainvert") {
        return true;
    }
    alpha.low_ratio >= 0.95 && alpha.high_ratio <= 0.03 && alpha.low_alpha_rgb_mean >= 8.0
}

fn invert_rgba_alpha_in_place(data: &mut [u8]) {
    for offset in (3..data.len()).step_by(4) {
        data[offset] = 255u8.saturating_sub(data[offset]);
    }
}

fn analysis_after_alpha_invert(alpha: &PngAnalysis) -> PngAnalysis {
    PngAnalysis {
        has_alpha: alpha.high_ratio > 0.0 || alpha.partial_ratio > 0.0,
        low_ratio: alpha.high_ratio,
        high_ratio: alpha.low_ratio,
        partial_ratio: alpha.partial_ratio,
        low_alpha_rgb_mean: 0.0,
        channel_range_max: alpha.channel_range_max,
        luminance_std_dev: alpha.luminance_std_dev,
        mean_r: alpha.mean_r,
        mean_g: alpha.mean_g,
        mean_b: alpha.mean_b,
        blue_dominance: alpha.blue_dominance,
    }
}

struct TextureSelectionAnalysisNative {
    is_likely_flat_color: bool,
    is_likely_normal_map: bool,
    srgb_confidence: &'static str,
}

fn analyze_texture_selection_native(
    color: &PngAnalysis,
    resource_key: &str,
    _is_likely_srgb: Option<bool>,
    srgb_confidence: &'static str,
) -> TextureSelectionAnalysisNative {
    if resource_key.contains("normal")
        || resource_key.contains("bump")
        || resource_key.contains("lightmap")
        || resource_key.contains("metal")
        || resource_key.contains("rough")
        || resource_key.contains("ao")
        || resource_key.contains("mask")
    {
        return TextureSelectionAnalysisNative {
            is_likely_flat_color: false,
            is_likely_normal_map: resource_key.contains("normal") || resource_key.contains("bump"),
            srgb_confidence,
        };
    }

    TextureSelectionAnalysisNative {
        is_likely_flat_color: color.channel_range_max <= 12
            || (color.luminance_std_dev <= 0.035
                && color.channel_range_max <= 24
                && !resource_key.contains("shadow")),
        is_likely_normal_map: color.mean_b >= 0.7
            && (color.mean_r - 0.5).abs() <= 0.18
            && (color.mean_g - 0.5).abs() <= 0.18
            && color.blue_dominance >= 0.12
            && color.channel_range_max <= 72
            && color.luminance_std_dev <= 0.12,
        srgb_confidence,
    }
}

fn score_texture_selection(resource_key: &str, analysis: &TextureSelectionAnalysisNative) -> i32 {
    let mut score = texture_name_priority(resource_key);
    if analysis.srgb_confidence == "srgb" {
        score += 120;
    }
    if analysis.srgb_confidence == "linear" {
        score -= 120;
    }
    if analysis.is_likely_normal_map {
        score -= 120;
    }
    if analysis.is_likely_flat_color {
        score -= 80;
    } else {
        score += 20;
    }
    score
}

fn texture_name_priority(resource_key: &str) -> i32 {
    let mut score = 0;
    if resource_key.contains("basecolor") || resource_key.contains("albedo") {
        score += 80;
    }
    if resource_key.contains("diffuse") {
        score += 60;
    }
    if resource_key.contains("color") {
        score += 25;
    }
    if resource_key.contains("shadow") {
        score -= 20;
    }
    if resource_key.contains("lightmap") {
        score -= 12;
    }
    if resource_key.contains("light") {
        score -= 10;
    }
    if resource_key.contains("metal")
        || resource_key.contains("rough")
        || resource_key.contains("ao")
    {
        score -= 24;
    }
    if resource_key.contains("mask") {
        score -= 28;
    }
    if resource_key.contains("normal") || resource_key.contains("bump") {
        score -= 60;
    }
    score
}

fn normalize_key(value: &str) -> String {
    value
        .chars()
        .filter_map(|char| {
            if char.is_ascii_alphanumeric() {
                Some(char.to_ascii_lowercase())
            } else {
                None
            }
        })
        .collect()
}

fn classify_srgb_confidence(is_likely_srgb: Option<bool>) -> &'static str {
    match is_likely_srgb {
        Some(true) => "srgb",
        Some(false) => "linear",
        None => "unknown",
    }
}

#[napi]
pub fn interleave_vertex_buffers(
    position: Buffer,
    position_stride: u32,
    blend: Buffer,
    blend_stride: u32,
    texcoord: Buffer,
    texcoord_stride: u32,
) -> napi::Result<Buffer> {
    let position_stride = position_stride as usize;
    let blend_stride = blend_stride as usize;
    let texcoord_stride = texcoord_stride as usize;
    if position_stride == 0 || blend_stride == 0 || texcoord_stride == 0 {
        return Err(napi::Error::from_reason(
            "Vertex buffer stride must be greater than zero",
        ));
    }

    let stride = position_stride + blend_stride + texcoord_stride;
    let vertex_count = (position.len() / position_stride)
        .min(blend.len() / blend_stride)
        .min(texcoord.len() / texcoord_stride);
    let mut out = vec![0u8; vertex_count * stride];

    for i in 0..vertex_count {
        let mut offset = i * stride;
        let src = i * position_stride;
        out[offset..offset + position_stride]
            .copy_from_slice(&position[src..src + position_stride]);
        offset += position_stride;
        let src = i * blend_stride;
        out[offset..offset + blend_stride].copy_from_slice(&blend[src..src + blend_stride]);
        offset += blend_stride;
        let src = i * texcoord_stride;
        out[offset..offset + texcoord_stride]
            .copy_from_slice(&texcoord[src..src + texcoord_stride]);
    }

    Ok(out.into())
}

#[napi]
pub fn decode_indices(bytes: Buffer, format: String) -> napi::Result<Buffer> {
    let upper = format.to_uppercase();
    let out = if upper.contains("R16_UINT") {
        if bytes.len() % 2 != 0 {
            return Err(napi::Error::from_reason(
                "R16_UINT index buffer has an odd byte length",
            ));
        }
        let mut values = Vec::with_capacity(bytes.len() / 2);
        for chunk in bytes.chunks_exact(2) {
            values.push(u16::from_le_bytes([chunk[0], chunk[1]]) as u32);
        }
        values
    } else if upper.contains("R32_UINT") || upper.contains("UNKNOWN") {
        if bytes.len() % 4 != 0 {
            return Err(napi::Error::from_reason(
                "R32_UINT index buffer byte length is not divisible by 4",
            ));
        }
        let mut values = Vec::with_capacity(bytes.len() / 4);
        for chunk in bytes.chunks_exact(4) {
            values.push(u32::from_le_bytes([chunk[0], chunk[1], chunk[2], chunk[3]]));
        }
        values
    } else {
        return Err(napi::Error::from_reason(format!(
            "Unsupported IB format: {format}"
        )));
    };

    Ok(u32_vec_to_buffer(out))
}

#[napi]
pub fn merge_draw_indices(
    indices: Buffer,
    draws: Vec<DrawRange>,
) -> napi::Result<MergeDrawIndicesResult> {
    let indices = buffer_to_u32_vec(&indices)?;
    let mut merged = Vec::new();
    let mut invalid_ranges = Vec::new();
    let has_draws = !draws.is_empty();

    for draw in draws {
        let start_index = draw.start_index;
        let index_count = draw.index_count;
        let end_index = start_index.saturating_add(index_count);
        if start_index < 0 || index_count < 0 || end_index < 0 || end_index as usize > indices.len()
        {
            invalid_ranges.push(format!(
                "Skipping invalid draw range start={} count={}",
                draw.start_index, draw.index_count
            ));
            continue;
        }

        for index in start_index as usize..end_index as usize {
            let value = indices[index] as i64 + draw.base_vertex as i64;
            if value < 0 {
                return Err(napi::Error::from_reason(format!(
                    "Merged index became negative for draw start={} count={} baseVertex={}",
                    draw.start_index, draw.index_count, draw.base_vertex
                )));
            }
            merged.push(value as u32);
        }
    }

    Ok(MergeDrawIndicesResult {
        indices: if has_draws {
            u32_vec_to_buffer(merged)
        } else {
            u32_vec_to_buffer(indices)
        },
        invalid_ranges,
    })
}

#[napi]
pub fn read_float_attribute(
    bytes: Buffer,
    stride: u32,
    vertex_count: u32,
    aligned_byte_offset: u32,
    format: String,
    width: u32,
) -> napi::Result<Buffer> {
    let stride = stride as usize;
    let vertex_count = vertex_count as usize;
    let aligned_byte_offset = aligned_byte_offset as usize;
    let width = width as usize;
    let mut out = Vec::with_capacity(vertex_count * width);

    for vertex in 0..vertex_count {
        let base = vertex * stride + aligned_byte_offset;
        let values = read_dxgi_values(&bytes, base, &format)?;
        for c in 0..width {
            out.push(*values.get(c).unwrap_or(&0.0));
        }
    }

    Ok(f32_vec_to_buffer(out))
}

#[napi]
pub fn ensure_vec4(
    data: Buffer,
    vertex_count: u32,
    width: u32,
    fill_w: f64,
) -> napi::Result<Buffer> {
    let data = buffer_to_f32_vec(&data)?;
    let vertex_count = vertex_count as usize;
    let width = width as usize;
    let fill_w = fill_w as f32;
    if width == 4 {
        return Ok(f32_vec_to_buffer(data));
    }

    let mut out = vec![0.0f32; vertex_count * 4];
    for i in 0..vertex_count {
        out[i * 4] = data.get(i * width).copied().unwrap_or(0.0);
        out[i * 4 + 1] = data.get(i * width + 1).copied().unwrap_or(0.0);
        out[i * 4 + 2] = data.get(i * width + 2).copied().unwrap_or(0.0);
        out[i * 4 + 3] = if width > 3 {
            data.get(i * width + 3).copied().unwrap_or(fill_w)
        } else {
            fill_w
        };
    }

    Ok(f32_vec_to_buffer(out))
}

#[napi]
pub fn normalize_vec3_array(data: Buffer) -> napi::Result<Buffer> {
    let data = buffer_to_f32_vec(&data)?;
    let mut out = vec![0.0f32; data.len()];
    for i in (0..data.len()).step_by(3) {
        let x = data.get(i).copied().unwrap_or(0.0);
        let y = data.get(i + 1).copied().unwrap_or(0.0);
        let z = data.get(i + 2).copied().unwrap_or(0.0);
        let length = (x * x + y * y + z * z).sqrt();
        if length > 1e-8 {
            out[i] = x / length;
            if i + 1 < out.len() {
                out[i + 1] = y / length;
            }
            if i + 2 < out.len() {
                out[i + 2] = z / length;
            }
        }
    }
    Ok(f32_vec_to_buffer(out))
}

#[napi]
pub fn normalize_tangent_array(data: Buffer) -> napi::Result<Buffer> {
    let data = buffer_to_f32_vec(&data)?;
    let mut out = vec![0.0f32; data.len()];
    for i in (0..data.len()).step_by(4) {
        let x = data.get(i).copied().unwrap_or(0.0);
        let y = data.get(i + 1).copied().unwrap_or(0.0);
        let z = data.get(i + 2).copied().unwrap_or(0.0);
        let length = (x * x + y * y + z * z).sqrt();
        if length > 1e-8 {
            out[i] = x / length;
            if i + 1 < out.len() {
                out[i + 1] = y / length;
            }
            if i + 2 < out.len() {
                out[i + 2] = z / length;
            }
        }
        if i + 3 < out.len() {
            out[i + 3] = if data.get(i + 3).copied().unwrap_or(0.0) >= 0.0 {
                1.0
            } else {
                -1.0
            };
        }
    }
    Ok(f32_vec_to_buffer(out))
}

#[napi]
pub fn remove_degenerate_triangles(
    indices: Buffer,
) -> napi::Result<RemoveDegenerateTrianglesResult> {
    let indices = buffer_to_u32_vec(&indices)?;
    let mut removed = 0u32;
    for i in (0..indices.len()).step_by(3) {
        if i + 2 >= indices.len() {
            break;
        }
        let a = indices[i];
        let b = indices[i + 1];
        let c = indices[i + 2];
        if a == b || b == c || a == c {
            removed += 1;
        }
    }

    if removed == 0 {
        return Ok(RemoveDegenerateTrianglesResult {
            indices: u32_vec_to_buffer(indices),
            removed,
        });
    }

    let mut out = Vec::with_capacity(indices.len() - removed as usize * 3);
    for i in (0..indices.len()).step_by(3) {
        if i + 2 >= indices.len() {
            break;
        }
        let a = indices[i];
        let b = indices[i + 1];
        let c = indices[i + 2];
        if a == b || b == c || a == c {
            continue;
        }
        out.push(a);
        out.push(b);
        out.push(c);
    }

    Ok(RemoveDegenerateTrianglesResult {
        indices: u32_vec_to_buffer(out),
        removed,
    })
}

fn ratio(value: usize, total: usize) -> f64 {
    if total == 0 {
        0.0
    } else {
        value as f64 / total as f64
    }
}

fn buffer_to_u32_vec(buffer: &Buffer) -> napi::Result<Vec<u32>> {
    if buffer.len() % 4 != 0 {
        return Err(napi::Error::from_reason(
            "Expected a Uint32-compatible buffer",
        ));
    }

    Ok(buffer
        .chunks_exact(4)
        .map(|chunk| u32::from_le_bytes([chunk[0], chunk[1], chunk[2], chunk[3]]))
        .collect())
}

fn buffer_to_f32_vec(buffer: &Buffer) -> napi::Result<Vec<f32>> {
    if buffer.len() % 4 != 0 {
        return Err(napi::Error::from_reason(
            "Expected a Float32-compatible buffer",
        ));
    }

    Ok(buffer
        .chunks_exact(4)
        .map(|chunk| f32::from_le_bytes([chunk[0], chunk[1], chunk[2], chunk[3]]))
        .collect())
}

fn u32_vec_to_buffer(values: Vec<u32>) -> Buffer {
    let mut out = Vec::with_capacity(values.len() * 4);
    for value in values {
        out.extend_from_slice(&value.to_le_bytes());
    }
    out.into()
}

fn f32_vec_to_buffer(values: Vec<f32>) -> Buffer {
    let mut out = Vec::with_capacity(values.len() * 4);
    for value in values {
        out.extend_from_slice(&value.to_le_bytes());
    }
    out.into()
}

fn read_dxgi_values(bytes: &[u8], offset: usize, format: &str) -> napi::Result<Vec<f32>> {
    let upper = format.to_uppercase();
    let count = format_component_count(&upper);

    if upper == "DXGI_FORMAT_R10G10B10A2_UNORM" {
        let value = read_u32(bytes, offset)?;
        return Ok(vec![
            (value & 0x3ff) as f32 / 1023.0,
            ((value >> 10) & 0x3ff) as f32 / 1023.0,
            ((value >> 20) & 0x3ff) as f32 / 1023.0,
            ((value >> 30) & 0x3) as f32 / 3.0,
        ]);
    }

    if upper.contains("_FLOAT") {
        if upper.contains("32") {
            return (0..count)
                .map(|i| read_f32(bytes, offset + i * 4))
                .collect::<napi::Result<Vec<_>>>();
        }
        if upper.contains("16") {
            return (0..count)
                .map(|i| read_u16(bytes, offset + i * 2).map(half_to_float))
                .collect::<napi::Result<Vec<_>>>();
        }
    }

    if upper.contains("_UNORM") {
        if upper.contains("16") {
            return (0..count)
                .map(|i| read_u16(bytes, offset + i * 2).map(|v| v as f32 / 65535.0))
                .collect::<napi::Result<Vec<_>>>();
        }
        if upper.contains("8") {
            return (0..count)
                .map(|i| read_u8(bytes, offset + i).map(|v| v as f32 / 255.0))
                .collect::<napi::Result<Vec<_>>>();
        }
    }

    if upper.contains("_SNORM") {
        if upper.contains("16") {
            return (0..count)
                .map(|i| read_i16(bytes, offset + i * 2).map(|v| (v as f32 / 32767.0).max(-1.0)))
                .collect::<napi::Result<Vec<_>>>();
        }
        if upper.contains("8") {
            return (0..count)
                .map(|i| read_i8(bytes, offset + i).map(|v| (v as f32 / 127.0).max(-1.0)))
                .collect::<napi::Result<Vec<_>>>();
        }
    }

    if upper.contains("_UINT") {
        if upper.contains("32") {
            return (0..count)
                .map(|i| read_u32(bytes, offset + i * 4).map(|v| v as f32))
                .collect::<napi::Result<Vec<_>>>();
        }
        if upper.contains("16") {
            return (0..count)
                .map(|i| read_u16(bytes, offset + i * 2).map(|v| v as f32))
                .collect::<napi::Result<Vec<_>>>();
        }
        if upper.contains("8") {
            return (0..count)
                .map(|i| read_u8(bytes, offset + i).map(|v| v as f32))
                .collect::<napi::Result<Vec<_>>>();
        }
    }

    if upper.contains("_SINT") {
        if upper.contains("32") {
            return (0..count)
                .map(|i| read_i32(bytes, offset + i * 4).map(|v| v as f32))
                .collect::<napi::Result<Vec<_>>>();
        }
        if upper.contains("16") {
            return (0..count)
                .map(|i| read_i16(bytes, offset + i * 2).map(|v| v as f32))
                .collect::<napi::Result<Vec<_>>>();
        }
        if upper.contains("8") {
            return (0..count)
                .map(|i| read_i8(bytes, offset + i).map(|v| v as f32))
                .collect::<napi::Result<Vec<_>>>();
        }
    }

    Err(napi::Error::from_reason(format!(
        "Unsupported DXGI format: {format}"
    )))
}

fn format_component_count(format: &str) -> usize {
    let normalized = format.trim_start_matches("DXGI_FORMAT_");
    let mut count = 0usize;
    let mut chars = normalized.chars().peekable();
    while let Some(char) = chars.next() {
        if matches!(char, 'R' | 'G' | 'B' | 'A') {
            let mut has_digits = false;
            while matches!(chars.peek(), Some(next) if next.is_ascii_digit()) {
                has_digits = true;
                chars.next();
            }
            if has_digits {
                count += 1;
            }
        }
    }
    if count == 0 {
        1
    } else {
        count
    }
}

fn half_to_float(value: u16) -> f32 {
    let sign = if value & 0x8000 != 0 { -1.0 } else { 1.0 };
    let exponent = ((value >> 10) & 0x1f) as i32;
    let fraction = (value & 0x03ff) as f32;
    if exponent == 0 {
        return sign * 2f32.powi(-14) * (fraction / 1024.0);
    }
    if exponent == 31 {
        return if fraction > 0.0 {
            f32::NAN
        } else {
            sign * f32::INFINITY
        };
    }
    sign * 2f32.powi(exponent - 15) * (1.0 + fraction / 1024.0)
}

fn read_u8(bytes: &[u8], offset: usize) -> napi::Result<u8> {
    bytes
        .get(offset)
        .copied()
        .ok_or_else(|| napi::Error::from_reason(format!("Out-of-bounds read at offset {offset}")))
}

fn read_i8(bytes: &[u8], offset: usize) -> napi::Result<i8> {
    read_u8(bytes, offset).map(|value| value as i8)
}

fn read_u16(bytes: &[u8], offset: usize) -> napi::Result<u16> {
    bytes
        .get(offset..offset + 2)
        .and_then(|slice| slice.try_into().ok())
        .map(u16::from_le_bytes)
        .ok_or_else(|| napi::Error::from_reason(format!("Out-of-bounds read at offset {offset}")))
}

fn read_i16(bytes: &[u8], offset: usize) -> napi::Result<i16> {
    read_u16(bytes, offset).map(|value| value as i16)
}

fn read_u32(bytes: &[u8], offset: usize) -> napi::Result<u32> {
    bytes
        .get(offset..offset + 4)
        .and_then(|slice| slice.try_into().ok())
        .map(u32::from_le_bytes)
        .ok_or_else(|| napi::Error::from_reason(format!("Out-of-bounds read at offset {offset}")))
}

fn read_i32(bytes: &[u8], offset: usize) -> napi::Result<i32> {
    read_u32(bytes, offset).map(|value| value as i32)
}

fn read_f32(bytes: &[u8], offset: usize) -> napi::Result<f32> {
    read_u32(bytes, offset).map(f32::from_bits)
}

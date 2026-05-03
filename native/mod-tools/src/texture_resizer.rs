use ddsfile::Dds;
use image::imageops::FilterType;
use image_dds::{ImageFormat, Mipmaps, Quality, Surface, SurfaceRgba32Float};
use napi::bindgen_prelude::AsyncTask;
use napi::Task;
use napi_derive::napi;
use std::fs::{self, File};
use std::io::{BufReader, BufWriter};
use std::path::{Path, PathBuf};

const MIN_DIMENSION: u32 = 1024;
const DIMENSION_STEP: u32 = 1024;
const DEFAULT_PERCENT: u32 = 50;

#[derive(Clone, Debug)]
#[napi(object)]
pub struct TextureResizeRequest {
    pub target_path: String,
    pub mode: String,
    pub operation: Option<String>,
    pub percent: Option<u32>,
    pub custom_width: Option<u32>,
    pub custom_height: Option<u32>,
    pub output_format: Option<String>,
    pub backup: Option<bool>,
}

#[derive(Clone, Debug)]
#[napi(object)]
pub struct TextureResizeFileResult {
    pub file_path: String,
    pub status: String,
    pub original_width: u32,
    pub original_height: u32,
    pub output_width: u32,
    pub output_height: u32,
    pub original_format: String,
    pub output_format: String,
    pub backup_created: bool,
    pub message: Option<String>,
}

#[derive(Clone, Debug)]
#[napi(object)]
pub struct TextureResizeResult {
    pub target_path: String,
    pub processed: u32,
    pub updated: u32,
    pub skipped: u32,
    pub failed: u32,
    pub files: Vec<TextureResizeFileResult>,
}

#[derive(Clone, Debug)]
enum ResizeMode {
    Percent(u32),
    Custom { max_width: u32, max_height: u32 },
}

#[derive(Clone, Debug, PartialEq, Eq)]
enum TextureOperation {
    Resize,
    ResizeAndConvert,
    Convert,
}

#[derive(Clone, Debug)]
struct NormalizedRequest {
    target_path: PathBuf,
    mode: ResizeMode,
    operation: TextureOperation,
    output_format: Option<ImageFormat>,
    backup: bool,
}

pub struct TextureResizeTask {
    request: NormalizedRequest,
}

#[napi]
impl Task for TextureResizeTask {
    type Output = TextureResizeResult;
    type JsValue = TextureResizeResult;

    fn compute(&mut self) -> napi::Result<Self::Output> {
        execute_resize(&self.request)
    }

    fn resolve(&mut self, _: napi::Env, output: Self::Output) -> napi::Result<Self::JsValue> {
        Ok(output)
    }
}

#[napi]
pub fn resize_textures(request: TextureResizeRequest) -> napi::Result<AsyncTask<TextureResizeTask>> {
    Ok(AsyncTask::new(TextureResizeTask {
        request: normalize_request(request)?,
    }))
}

fn normalize_request(request: TextureResizeRequest) -> napi::Result<NormalizedRequest> {
    let target_path = PathBuf::from(request.target_path.trim());
    if request.target_path.trim().is_empty() {
        return Err(napi::Error::from_reason("Target path is required."));
    }

    let mode = match request.mode.as_str() {
        "percent" => ResizeMode::Percent(
            request
                .percent
                .filter(|value| (1..100).contains(value))
                .unwrap_or(DEFAULT_PERCENT),
        ),
        "custom" => {
            let max_width = normalize_dimension(request.custom_width.unwrap_or(MIN_DIMENSION));
            let max_height = normalize_dimension(request.custom_height.unwrap_or(MIN_DIMENSION));
            ResizeMode::Custom {
                max_width,
                max_height,
            }
        }
        _ => {
            return Err(napi::Error::from_reason(format!(
                "Unsupported resize mode '{}'.",
                request.mode
            )));
        }
    };

    let operation = match request.operation.as_deref() {
        Some("resize_and_convert") => TextureOperation::ResizeAndConvert,
        Some("convert") => TextureOperation::Convert,
        Some("resize") | None => TextureOperation::Resize,
        Some(value) => {
            return Err(napi::Error::from_reason(format!(
                "Unsupported texture operation '{}'.",
                value
            )));
        }
    };

    let output_format = request
        .output_format
        .as_deref()
        .filter(|value| !value.trim().is_empty())
        .map(parse_output_format)
        .transpose()
        .map_err(napi::Error::from_reason)?;

    Ok(NormalizedRequest {
        target_path,
        mode,
        operation,
        output_format,
        backup: request.backup.unwrap_or(true),
    })
}

fn normalize_dimension(value: u32) -> u32 {
    if value <= MIN_DIMENSION {
        return MIN_DIMENSION;
    }

    let remainder = value % DIMENSION_STEP;
    if remainder == 0 {
        value
    } else if remainder >= DIMENSION_STEP / 2 {
        value + (DIMENSION_STEP - remainder)
    } else {
        value - remainder
    }
}

fn execute_resize(request: &NormalizedRequest) -> napi::Result<TextureResizeResult> {
    let target_path = request.target_path.canonicalize().map_err(|error| {
        napi::Error::from_reason(format!(
            "Failed to resolve target path '{}': {}",
            request.target_path.display(),
            error
        ))
    })?;

    if !target_path.is_dir() && !target_path.is_file() {
        return Err(napi::Error::from_reason(format!(
            "Target path '{}' must be a directory or DDS file.",
            target_path.display()
        )));
    }

    let mut files = Vec::new();
    if target_path.is_file() {
        if !target_path
            .extension()
            .and_then(|extension| extension.to_str())
            .is_some_and(|extension| extension.eq_ignore_ascii_case("dds"))
        {
            return Err(napi::Error::from_reason(format!(
                "Target file '{}' must be a DDS texture.",
                target_path.display()
            )));
        }

        files.push(target_path.clone());
    } else {
        collect_dds_files(&target_path, &mut files).map_err(|error| {
            napi::Error::from_reason(format!(
                "Failed to scan target directory '{}': {}",
                target_path.display(),
                error
            ))
        })?;
    }
    files.sort();

    let mut results = Vec::with_capacity(files.len());
    let mut updated = 0u32;
    let mut skipped = 0u32;
    let mut failed = 0u32;

    for file_path in files {
        let result = match resize_single_file(&file_path, request) {
            Ok(result) => result,
            Err(error) => TextureResizeFileResult {
                file_path: file_path.to_string_lossy().to_string(),
                status: "failed".to_string(),
                original_width: 0,
                original_height: 0,
                output_width: 0,
                output_height: 0,
                original_format: "UNKNOWN_DDS_FORMAT".to_string(),
                output_format: "UNKNOWN_DDS_FORMAT".to_string(),
                backup_created: false,
                message: Some(error),
            },
        };

        match result.status.as_str() {
            "updated" => updated += 1,
            "skipped" => skipped += 1,
            _ => failed += 1,
        }
        results.push(result);
    }

    Ok(TextureResizeResult {
        target_path: target_path.to_string_lossy().to_string(),
        processed: results.len() as u32,
        updated,
        skipped,
        failed,
        files: results,
    })
}

fn collect_dds_files(root: &Path, files: &mut Vec<PathBuf>) -> std::io::Result<()> {
    for entry in fs::read_dir(root)? {
        let entry = entry?;
        let path = entry.path();
        let file_type = entry.file_type()?;
        if file_type.is_dir() {
            collect_dds_files(&path, files)?;
            continue;
        }

        if file_type.is_file()
            && path
                .extension()
                .and_then(|extension| extension.to_str())
                .is_some_and(|extension| extension.eq_ignore_ascii_case("dds"))
        {
            files.push(path);
        }
    }

    Ok(())
}

fn resize_single_file(path: &Path, request: &NormalizedRequest) -> Result<TextureResizeFileResult, String> {
    let file = File::open(path)
        .map_err(|error| format!("Failed to open DDS file '{}': {}", path.display(), error))?;
    let mut reader = BufReader::new(file);
    let dds = Dds::read(&mut reader)
        .map_err(|error| format!("Failed to read DDS file '{}': {}", path.display(), error))?;
    let surface = Surface::from_dds(&dds)
        .map_err(|error| format!("Failed to decode DDS metadata '{}': {}", path.display(), error))?;

    let original_width = surface.width;
    let original_height = surface.height;
    let original_format = image_format_name(surface.image_format).to_string();
    let output_format = request.output_format.unwrap_or(surface.image_format);
    let output_format_name = image_format_name(output_format).to_string();

    let resize_target = match request.operation {
        TextureOperation::Convert => Some((original_width, original_height)),
        TextureOperation::Resize | TextureOperation::ResizeAndConvert => {
            calculate_target_dimensions(original_width, original_height, &request.mode)
        }
    };

    if request.operation == TextureOperation::Convert && output_format == surface.image_format {
        return Ok(TextureResizeFileResult {
            file_path: path.to_string_lossy().to_string(),
            status: "skipped".to_string(),
            original_width,
            original_height,
            output_width: original_width,
            output_height: original_height,
            original_format: original_format.clone(),
            output_format: output_format_name,
            backup_created: false,
            message: Some("Selected output format matches the source format.".to_string()),
        });
    }

    let Some((target_width, target_height)) = resize_target else {
        return Ok(TextureResizeFileResult {
            file_path: path.to_string_lossy().to_string(),
            status: "skipped".to_string(),
            original_width,
            original_height,
            output_width: original_width,
            output_height: original_height,
            original_format: original_format.clone(),
            output_format: output_format_name,
            backup_created: false,
            message: Some("No valid downscale candidate matched the requested bounds.".to_string()),
        });
    };

    let decoded = surface
        .decode_rgbaf32()
        .map_err(|error| format!("Failed to decode DDS pixels '{}': {}", path.display(), error))?;
    let output_surface = if target_width == original_width && target_height == original_height {
        decoded
    } else {
        resize_surface_rgba32f(&decoded, surface.layers, surface.depth, target_width, target_height)
            .map_err(|error| format!("Failed to resize DDS '{}': {}", path.display(), error))?
    };

    let mipmaps = if surface.mipmaps > 1 {
        Mipmaps::GeneratedAutomatic
    } else {
        Mipmaps::Disabled
    };

    let encoded = output_surface
        .encode(output_format, Quality::Fast, mipmaps)
        .map_err(|error| {
            format!(
                "Failed to encode processed DDS '{}': {}",
                path.display(),
                error
            )
        })?;
    let resized_dds = encoded
        .to_dds()
        .map_err(|error| format!("Failed to create DDS '{}': {}", path.display(), error))?;

    let backup_created = if request.backup {
        create_backup_if_missing(path)?
    } else {
        false
    };

    let output = File::create(path)
        .map_err(|error| format!("Failed to overwrite DDS file '{}': {}", path.display(), error))?;
    let mut writer = BufWriter::new(output);
    resized_dds
        .write(&mut writer)
        .map_err(|error| format!("Failed to write DDS file '{}': {}", path.display(), error))?;

    let message = if request.operation == TextureOperation::Convert {
        Some("Format changed without resizing.".to_string())
    } else if output_format != surface.image_format && target_width == original_width && target_height == original_height {
        Some("Texture format changed without resizing.".to_string())
    } else {
        None
    };

    Ok(TextureResizeFileResult {
        file_path: path.to_string_lossy().to_string(),
        status: "updated".to_string(),
        original_width,
        original_height,
        output_width: target_width,
        output_height: target_height,
        original_format,
        output_format: output_format_name,
        backup_created,
        message,
    })
}

fn resize_surface_rgba32f(
    surface: &SurfaceRgba32Float<Vec<f32>>,
    layers: u32,
    depth: u32,
    target_width: u32,
    target_height: u32,
) -> Result<SurfaceRgba32Float<Vec<f32>>, String> {
    let mut data = Vec::new();

    for layer in 0..layers {
        for depth_level in 0..depth {
            let image = surface
                .get_image(layer, depth_level, 0)
                .ok_or_else(|| "Missing base mip image data.".to_string())?;
            let resized = image::imageops::resize(&image, target_width, target_height, FilterType::Triangle);
            data.extend_from_slice(resized.as_raw());
        }
    }

    Ok(SurfaceRgba32Float {
        width: target_width,
        height: target_height,
        depth,
        layers,
        mipmaps: 1,
        data,
    })
}

fn create_backup_if_missing(path: &Path) -> Result<bool, String> {
    let backup_path = backup_path(path);
    if backup_path.exists() {
        return Ok(false);
    }

    fs::copy(path, &backup_path).map_err(|error| {
        format!(
            "Failed to create backup file '{}': {}",
            backup_path.display(),
            error
        )
    })?;
    Ok(true)
}

fn backup_path(path: &Path) -> PathBuf {
    PathBuf::from(format!("{}.bak", path.to_string_lossy()))
}

fn calculate_target_dimensions(
    width: u32,
    height: u32,
    mode: &ResizeMode,
) -> Option<(u32, u32)> {
    let candidates = valid_downscale_candidates(width, height);
    if candidates.is_empty() {
        return None;
    }

    let (max_width, max_height) = match mode {
        ResizeMode::Percent(percent) => (
            width.saturating_mul(*percent) / 100,
            height.saturating_mul(*percent) / 100,
        ),
        ResizeMode::Custom {
            max_width,
            max_height,
        } => (*max_width, *max_height),
    };

    candidates
        .into_iter()
        .filter(|(candidate_width, candidate_height)| {
            *candidate_width <= max_width && *candidate_height <= max_height
        })
        .max_by_key(|(candidate_width, candidate_height)| candidate_width * candidate_height)
}

fn valid_downscale_candidates(width: u32, height: u32) -> Vec<(u32, u32)> {
    if width < MIN_DIMENSION || height < MIN_DIMENSION {
        return Vec::new();
    }

    let gcd = gcd(width, height);
    let ratio_width = width / gcd;
    let ratio_height = height / gcd;

    if ratio_width == 0 || ratio_height == 0 {
        return Vec::new();
    }

    let max_scale = (width / (ratio_width * DIMENSION_STEP)).min(height / (ratio_height * DIMENSION_STEP));
    if max_scale == 0 {
        return Vec::new();
    }

    (1..=max_scale)
        .map(|scale| {
            (
                ratio_width * DIMENSION_STEP * scale,
                ratio_height * DIMENSION_STEP * scale,
            )
        })
        .filter(|(candidate_width, candidate_height)| {
            *candidate_width < width || *candidate_height < height
        })
        .collect()
}

fn gcd(mut left: u32, mut right: u32) -> u32 {
    while right != 0 {
        let remainder = left % right;
        left = right;
        right = remainder;
    }
    left
}

fn parse_output_format(value: &str) -> Result<ImageFormat, String> {
    match value {
        "DXGI_FORMAT_R8_UNORM" => Ok(ImageFormat::R8Unorm),
        "DXGI_FORMAT_R8_SNORM" => Ok(ImageFormat::R8Snorm),
        "DXGI_FORMAT_R8G8_UNORM" => Ok(ImageFormat::Rg8Unorm),
        "DXGI_FORMAT_R8G8_SNORM" => Ok(ImageFormat::Rg8Snorm),
        "DXGI_FORMAT_R8G8B8A8_UNORM" => Ok(ImageFormat::Rgba8Unorm),
        "DXGI_FORMAT_R8G8B8A8_UNORM_SRGB" => Ok(ImageFormat::Rgba8UnormSrgb),
        "DXGI_FORMAT_R8G8B8A8_SNORM" => Ok(ImageFormat::Rgba8Snorm),
        "DXGI_FORMAT_R16_UNORM" => Ok(ImageFormat::R16Unorm),
        "DXGI_FORMAT_R16_SNORM" => Ok(ImageFormat::R16Snorm),
        "DXGI_FORMAT_R16_FLOAT" => Ok(ImageFormat::R16Float),
        "DXGI_FORMAT_R16G16_UNORM" => Ok(ImageFormat::Rg16Unorm),
        "DXGI_FORMAT_R16G16_SNORM" => Ok(ImageFormat::Rg16Snorm),
        "DXGI_FORMAT_R16G16_FLOAT" => Ok(ImageFormat::Rg16Float),
        "DXGI_FORMAT_R16G16B16A16_UNORM" => Ok(ImageFormat::Rgba16Unorm),
        "DXGI_FORMAT_R16G16B16A16_SNORM" => Ok(ImageFormat::Rgba16Snorm),
        "DXGI_FORMAT_R16G16B16A16_FLOAT" => Ok(ImageFormat::Rgba16Float),
        "DXGI_FORMAT_R32_FLOAT" => Ok(ImageFormat::R32Float),
        "DXGI_FORMAT_R32G32_FLOAT" => Ok(ImageFormat::Rg32Float),
        "DXGI_FORMAT_R32G32B32_FLOAT" => Ok(ImageFormat::Rgb32Float),
        "DXGI_FORMAT_R32G32B32A32_FLOAT" => Ok(ImageFormat::Rgba32Float),
        "DXGI_FORMAT_B8G8R8A8_UNORM" => Ok(ImageFormat::Bgra8Unorm),
        "DXGI_FORMAT_B8G8R8A8_UNORM_SRGB" => Ok(ImageFormat::Bgra8UnormSrgb),
        "DXGI_FORMAT_B4G4R4A4_UNORM" => Ok(ImageFormat::Bgra4Unorm),
        "DXGI_FORMAT_B5G5R5A1_UNORM" => Ok(ImageFormat::Bgr5A1Unorm),
        "DXGI_FORMAT_BC1_UNORM" => Ok(ImageFormat::BC1RgbaUnorm),
        "DXGI_FORMAT_BC1_UNORM_SRGB" => Ok(ImageFormat::BC1RgbaUnormSrgb),
        "DXGI_FORMAT_BC2_UNORM" => Ok(ImageFormat::BC2RgbaUnorm),
        "DXGI_FORMAT_BC2_UNORM_SRGB" => Ok(ImageFormat::BC2RgbaUnormSrgb),
        "DXGI_FORMAT_BC3_UNORM" => Ok(ImageFormat::BC3RgbaUnorm),
        "DXGI_FORMAT_BC3_UNORM_SRGB" => Ok(ImageFormat::BC3RgbaUnormSrgb),
        "DXGI_FORMAT_BC4_UNORM" => Ok(ImageFormat::BC4RUnorm),
        "DXGI_FORMAT_BC4_SNORM" => Ok(ImageFormat::BC4RSnorm),
        "DXGI_FORMAT_BC5_UNORM" => Ok(ImageFormat::BC5RgUnorm),
        "DXGI_FORMAT_BC5_SNORM" => Ok(ImageFormat::BC5RgSnorm),
        "DXGI_FORMAT_BC6H_UF16" => Ok(ImageFormat::BC6hRgbUfloat),
        "DXGI_FORMAT_BC6H_SF16" => Ok(ImageFormat::BC6hRgbSfloat),
        "DXGI_FORMAT_BC7_UNORM" => Ok(ImageFormat::BC7RgbaUnorm),
        "DXGI_FORMAT_BC7_UNORM_SRGB" => Ok(ImageFormat::BC7RgbaUnormSrgb),
        _ => Err(format!("Unsupported output format '{}'.", value)),
    }
}

fn image_format_name(format: ImageFormat) -> &'static str {
    match format {
        ImageFormat::R8Unorm => "DXGI_FORMAT_R8_UNORM",
        ImageFormat::R8Snorm => "DXGI_FORMAT_R8_SNORM",
        ImageFormat::Rg8Unorm => "DXGI_FORMAT_R8G8_UNORM",
        ImageFormat::Rg8Snorm => "DXGI_FORMAT_R8G8_SNORM",
        ImageFormat::Rgba8Unorm => "DXGI_FORMAT_R8G8B8A8_UNORM",
        ImageFormat::Rgba8UnormSrgb => "DXGI_FORMAT_R8G8B8A8_UNORM_SRGB",
        ImageFormat::Rgba8Snorm => "DXGI_FORMAT_R8G8B8A8_SNORM",
        ImageFormat::R16Unorm => "DXGI_FORMAT_R16_UNORM",
        ImageFormat::R16Snorm => "DXGI_FORMAT_R16_SNORM",
        ImageFormat::R16Float => "DXGI_FORMAT_R16_FLOAT",
        ImageFormat::Rg16Unorm => "DXGI_FORMAT_R16G16_UNORM",
        ImageFormat::Rg16Snorm => "DXGI_FORMAT_R16G16_SNORM",
        ImageFormat::Rg16Float => "DXGI_FORMAT_R16G16_FLOAT",
        ImageFormat::Rgba16Unorm => "DXGI_FORMAT_R16G16B16A16_UNORM",
        ImageFormat::Rgba16Snorm => "DXGI_FORMAT_R16G16B16A16_SNORM",
        ImageFormat::Rgba16Float => "DXGI_FORMAT_R16G16B16A16_FLOAT",
        ImageFormat::R32Float => "DXGI_FORMAT_R32_FLOAT",
        ImageFormat::Rg32Float => "DXGI_FORMAT_R32G32_FLOAT",
        ImageFormat::Rgb32Float => "DXGI_FORMAT_R32G32B32_FLOAT",
        ImageFormat::Rgba32Float => "DXGI_FORMAT_R32G32B32A32_FLOAT",
        ImageFormat::Bgra8Unorm => "DXGI_FORMAT_B8G8R8A8_UNORM",
        ImageFormat::Bgra8UnormSrgb => "DXGI_FORMAT_B8G8R8A8_UNORM_SRGB",
        ImageFormat::Bgra4Unorm => "DXGI_FORMAT_B4G4R4A4_UNORM",
        ImageFormat::Bgr5A1Unorm => "DXGI_FORMAT_B5G5R5A1_UNORM",
        ImageFormat::BC1RgbaUnorm => "DXGI_FORMAT_BC1_UNORM",
        ImageFormat::BC1RgbaUnormSrgb => "DXGI_FORMAT_BC1_UNORM_SRGB",
        ImageFormat::BC2RgbaUnorm => "DXGI_FORMAT_BC2_UNORM",
        ImageFormat::BC2RgbaUnormSrgb => "DXGI_FORMAT_BC2_UNORM_SRGB",
        ImageFormat::BC3RgbaUnorm => "DXGI_FORMAT_BC3_UNORM",
        ImageFormat::BC3RgbaUnormSrgb => "DXGI_FORMAT_BC3_UNORM_SRGB",
        ImageFormat::BC4RUnorm => "DXGI_FORMAT_BC4_UNORM",
        ImageFormat::BC4RSnorm => "DXGI_FORMAT_BC4_SNORM",
        ImageFormat::BC5RgUnorm => "DXGI_FORMAT_BC5_UNORM",
        ImageFormat::BC5RgSnorm => "DXGI_FORMAT_BC5_SNORM",
        ImageFormat::BC6hRgbUfloat => "DXGI_FORMAT_BC6H_UF16",
        ImageFormat::BC6hRgbSfloat => "DXGI_FORMAT_BC6H_SF16",
        ImageFormat::BC7RgbaUnorm => "DXGI_FORMAT_BC7_UNORM",
        ImageFormat::BC7RgbaUnormSrgb => "DXGI_FORMAT_BC7_UNORM_SRGB",
        _ => "UNKNOWN_DDS_FORMAT",
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn calculates_square_candidates() {
        assert_eq!(
            valid_downscale_candidates(4096, 4096),
            vec![(1024, 1024), (2048, 2048), (3072, 3072)]
        );
    }

    #[test]
    fn calculates_wide_candidates() {
        assert_eq!(valid_downscale_candidates(4096, 2048), vec![(2048, 1024)]);
    }

    #[test]
    fn returns_no_candidates_when_ratio_cannot_fit_step() {
        assert!(valid_downscale_candidates(2048, 1024).is_empty());
    }

    #[test]
    fn percent_mode_picks_nearest_smaller_candidate() {
        let result = calculate_target_dimensions(4096, 4096, &ResizeMode::Percent(60));
        assert_eq!(result, Some((2048, 2048)));
    }

    #[test]
    fn custom_mode_respects_bounds() {
        let result = calculate_target_dimensions(
            4096,
            2048,
            &ResizeMode::Custom {
                max_width: 3072,
                max_height: 2048,
            },
        );
        assert_eq!(result, Some((2048, 1024)));
    }

    #[test]
    fn percent_mode_never_upscales() {
        let result = calculate_target_dimensions(1024, 1024, &ResizeMode::Percent(50));
        assert_eq!(result, None);
    }

    #[test]
    fn backup_path_uses_sibling_bak_file() {
        let path = PathBuf::from("C:\\mods\\example.dds");
        assert_eq!(
            backup_path(&path).to_string_lossy(),
            "C:\\mods\\example.dds.bak"
        );
    }

    #[test]
    fn normalizes_dimension_to_nearest_step() {
        assert_eq!(normalize_dimension(1500), 1024);
        assert_eq!(normalize_dimension(1700), 2048);
        assert_eq!(normalize_dimension(512), 1024);
    }

    #[test]
    fn parse_output_format_supports_bc7_srgb() {
        assert_eq!(
            parse_output_format("DXGI_FORMAT_BC7_UNORM_SRGB").unwrap(),
            ImageFormat::BC7RgbaUnormSrgb
        );
    }

    #[test]
    fn convert_operation_keeps_original_dimensions() {
        let request = NormalizedRequest {
            target_path: PathBuf::from("C:\\mods"),
            mode: ResizeMode::Percent(50),
            operation: TextureOperation::Convert,
            output_format: Some(ImageFormat::BC7RgbaUnorm),
            backup: true,
        };

        assert_eq!(request.operation, TextureOperation::Convert);
        assert_eq!(request.output_format, Some(ImageFormat::BC7RgbaUnorm));
    }
}

export const MENU_MAKER_CROP_PREVIEW_SIZE = 256;

export function coveredImageDrawRect(
    imageWidth: number,
    imageHeight: number,
    outputSize: number,
    zoom: number,
    offset: { x: number; y: number },
    previewSize = MENU_MAKER_CROP_PREVIEW_SIZE,
): { x: number; y: number; width: number; height: number } {
    const width = Math.max(1, imageWidth);
    const height = Math.max(1, imageHeight);
    const scale = Math.max(outputSize / width, outputSize / height) * zoom;
    return {
        x: (outputSize - width * scale) / 2 + (offset.x * outputSize) / previewSize,
        y: (outputSize - height * scale) / 2 + (offset.y * outputSize) / previewSize,
        width: width * scale,
        height: height * scale,
    };
}

export function drawCoveredImage(
    context: CanvasRenderingContext2D,
    image: CanvasImageSource,
    imageWidth: number,
    imageHeight: number,
    outputSize: number,
    zoom: number,
    offset: { x: number; y: number },
    previewSize = MENU_MAKER_CROP_PREVIEW_SIZE,
): void {
    const rect = coveredImageDrawRect(
        imageWidth,
        imageHeight,
        outputSize,
        zoom,
        offset,
        previewSize,
    );
    context.clearRect(0, 0, outputSize, outputSize);
    context.drawImage(image, rect.x, rect.y, rect.width, rect.height);
}

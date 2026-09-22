// Рендер на canvas и кодирование для общего кроппера аватара.

export const AVATAR_OUTPUT_SIZE = 512;

const AVATAR_QUALITY = 0.85;

export interface PixelCrop {
  x: number;
  y: number;
  width: number;
  height: number;
}

let webpEncodeSupport: boolean | null = null;

export function supportsWebpEncode(): boolean {
  if (webpEncodeSupport !== null) return webpEncodeSupport;
  try {
    const canvas = document.createElement('canvas');
    canvas.width = 1;
    canvas.height = 1;
    webpEncodeSupport = canvas.toDataURL('image/webp').startsWith('data:image/webp');
  } catch {
    webpEncodeSupport = false;
  }
  return webpEncodeSupport;
}

export function pickOutputType(): { type: string; quality: number } {
  return supportsWebpEncode()
    ? { type: 'image/webp', quality: AVATAR_QUALITY }
    : { type: 'image/jpeg', quality: AVATAR_QUALITY };
}

export function blobToAvatarFile(blob: Blob, sourceName: string, type: string): File {
  const ext = type === 'image/webp' ? 'webp' : type === 'image/png' ? 'png' : 'jpg';
  const base = sourceName.replace(/\.[^./\\]+$/, '') || 'avatar';
  return new File([blob], `${base}.${ext}`, { type });
}

function canvasToBlob(canvas: HTMLCanvasElement, type: string, quality: number): Promise<Blob> {
  return new Promise((resolve, reject) => {
    canvas.toBlob(
      (blob) => (blob ? resolve(blob) : reject(new Error('Canvas is empty'))),
      type,
      quality,
    );
  });
}

function loadImage(src: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const image = new Image();
    image.addEventListener('load', () => resolve(image));
    image.addEventListener('error', () => reject(new Error('Could not load image')));
    image.src = src;
  });
}

function toRadians(degrees: number): number {
  return (degrees * Math.PI) / 180;
}

function rotatedBoundingBox(width: number, height: number, degrees: number) {
  const rad = toRadians(degrees);
  return {
    width: Math.abs(Math.cos(rad) * width) + Math.abs(Math.sin(rad) * height),
    height: Math.abs(Math.sin(rad) * width) + Math.abs(Math.cos(rad) * height),
  };
}

export interface RenderOptions {
  output: number;
  type: string;
  quality: number;
  background?: string;
}

export async function getCroppedAvatarBlob(
  imageSrc: string,
  pixelCrop: PixelCrop,
  rotation: number,
  options: RenderOptions,
): Promise<Blob> {
  const image = await loadImage(imageSrc);
  const bBox = rotatedBoundingBox(image.width, image.height, rotation);

  const rotated = document.createElement('canvas');
  rotated.width = Math.round(bBox.width);
  rotated.height = Math.round(bBox.height);
  const rctx = rotated.getContext('2d');
  if (!rctx) throw new Error('Canvas 2D context unavailable');
  rctx.translate(rotated.width / 2, rotated.height / 2);
  rctx.rotate(toRadians(rotation));
  rctx.drawImage(image, -image.width / 2, -image.height / 2);

  const out = document.createElement('canvas');
  out.width = options.output;
  out.height = options.output;
  const octx = out.getContext('2d');
  if (!octx) throw new Error('Canvas 2D context unavailable');
  octx.imageSmoothingQuality = 'high';
  if (options.background) {
    octx.fillStyle = options.background;
    octx.fillRect(0, 0, options.output, options.output);
  }
  octx.drawImage(
    rotated,
    pixelCrop.x,
    pixelCrop.y,
    pixelCrop.width,
    pixelCrop.height,
    0,
    0,
    options.output,
    options.output,
  );
  return canvasToBlob(out, options.type, options.quality);
}

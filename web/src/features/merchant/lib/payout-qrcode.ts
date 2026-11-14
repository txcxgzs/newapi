/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

export const MAX_PAYOUT_QR_DATA_URL_LENGTH = 480_000

export const OPERATION_TENURE_OPTIONS = [
  { value: '<3m', labelKey: 'Less than 3 months' },
  { value: '3-6m', labelKey: '3 - 6 months' },
  { value: '6-12m', labelKey: '6 - 12 months' },
  { value: '1-2y', labelKey: '1 - 2 years' },
  { value: '2-3y', labelKey: '2 - 3 years' },
  { value: '3y+', labelKey: 'More than 3 years' },
] as const

const ALLOWED_IMAGE_MIME = new Set([
  'image/png',
  'image/jpeg',
  'image/jpg',
  'image/webp',
])

function readFileAsDataUrl(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.addEventListener('load', () => {
      if (typeof reader.result === 'string') {
        resolve(reader.result)
      } else {
        reject(new Error('Failed to read QR code image'))
      }
    })
    reader.addEventListener('error', () =>
      reject(new Error('Failed to read QR code image'))
    )
    reader.readAsDataURL(file)
  })
}

function loadImage(src: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const img = new Image()
    img.addEventListener('load', () => resolve(img))
    img.addEventListener('error', () =>
      reject(new Error('Invalid QR code image file'))
    )
    img.src = src
  })
}

/**
 * Compresses and normalizes a payout QR code image into a PNG/JPEG/WebP Data URL
 * bounded under MAX_PAYOUT_QR_DATA_URL_LENGTH so it fits safely in backend validation.
 */
export async function compressPayoutQrCodeFile(file: File): Promise<string> {
  if (!ALLOWED_IMAGE_MIME.has(file.type.toLowerCase())) {
    throw new Error('Only PNG, JPG, and WebP QR code images are supported')
  }
  const rawDataUrl = await readFileAsDataUrl(file)
  if (rawDataUrl.length <= 220_000) {
    return rawDataUrl
  }

  const img = await loadImage(rawDataUrl)
  const maxDim = 640
  const scale = Math.min(1, maxDim / Math.max(img.width || 1, img.height || 1))
  const width = Math.max(1, Math.round((img.width || maxDim) * scale))
  const height = Math.max(1, Math.round((img.height || maxDim) * scale))

  const canvas = document.createElement('canvas')
  canvas.width = width
  canvas.height = height
  const ctx = canvas.getContext('2d')
  if (!ctx) {
    if (rawDataUrl.length <= MAX_PAYOUT_QR_DATA_URL_LENGTH) {
      return rawDataUrl
    }
    throw new Error('Image is too large to process')
  }
  // White backdrop preserves QR module contrast when converting transparent PNGs.
  ctx.fillStyle = '#ffffff'
  ctx.fillRect(0, 0, width, height)
  ctx.drawImage(img, 0, 0, width, height)

  for (const quality of [0.88, 0.76, 0.64, 0.5]) {
    const webp = canvas.toDataURL('image/webp', quality)
    if (
      webp.startsWith('data:image/webp') &&
      webp.length <= MAX_PAYOUT_QR_DATA_URL_LENGTH
    ) {
      return webp
    }
    const jpeg = canvas.toDataURL('image/jpeg', quality)
    if (jpeg.length <= MAX_PAYOUT_QR_DATA_URL_LENGTH) {
      return jpeg
    }
  }
  throw new Error('QR code image is still too large after compression')
}

export function isValidPayoutQrDataUrl(value: string | undefined): boolean {
  if (!value) return false
  return /^data:image\/(png|jpeg|jpg|webp);base64,/i.test(value.trim())
}

// Regenerate the Tongxi app icon: swift scripts/generate-appicon.swift
import AppKit

let size = 1024
let bitmap = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: size, pixelsHigh: size,
    bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
    colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0)!
NSGraphicsContext.saveGraphicsState()
NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: bitmap)

let ink = NSColor(srgbRed: 32 / 255, green: 107 / 255, blue: 97 / 255, alpha: 1)
let paper = NSColor(srgbRed: 247 / 255, green: 249 / 255, blue: 251 / 255, alpha: 1)
let pale = NSColor(srgbRed: 229 / 255, green: 240 / 255, blue: 237 / 255, alpha: 1)

paper.setFill()
NSBezierPath(roundedRect: NSRect(x: 64, y: 64, width: 896, height: 896),
    xRadius: 200, yRadius: 200).fill()

// Four seats around the shared table, matching the mark in the app sidebar.
ink.setFill()
for rect in [NSRect(x: 440, y: 186, width: 144, height: 48),
             NSRect(x: 440, y: 790, width: 144, height: 48),
             NSRect(x: 186, y: 440, width: 48, height: 144),
             NSRect(x: 790, y: 440, width: 48, height: 144)] {
    NSBezierPath(roundedRect: rect, xRadius: 24, yRadius: 24).fill()
}

NSGraphicsContext.saveGraphicsState()
let transform = AffineTransform(translationByX: 512, byY: 512)
var rotated = transform
rotated.rotate(byDegrees: 45)
(rotated as NSAffineTransform).concat()
let table = NSBezierPath(roundedRect: NSRect(x: -160, y: -160, width: 320, height: 320),
    xRadius: 60, yRadius: 60)
pale.setFill()
table.fill()
ink.setStroke()
table.lineWidth = 24
table.stroke()
NSGraphicsContext.restoreGraphicsState()
NSGraphicsContext.restoreGraphicsState()

let output = URL(fileURLWithPath: "build/appicon.png")
try bitmap.representation(using: .png, properties: [:])!.write(to: output)
print("Generated \(output.path)")

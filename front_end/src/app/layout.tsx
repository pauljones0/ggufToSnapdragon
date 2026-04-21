import type { Metadata } from "next"

export const metadata: Metadata = {
    title: "HexForge Matrix",
    description: "Cloud-native pipeline resolving GGUF structures into Qualcomm Hexagon execution graphs for Snapdragon NPUs",
}

export default function RootLayout({
    children,
}: {
    children: React.ReactNode
}) {
    return (
        <html lang="en">
            <body>{children}</body>
        </html>
    )
}

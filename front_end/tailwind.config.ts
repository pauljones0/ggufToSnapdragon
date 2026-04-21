import type { Config } from 'tailwindcss'

const config: Config = {
    content: [
        './src/pages/**/*.{js,ts,jsx,tsx,mdx}',
        './src/components/**/*.{js,ts,jsx,tsx,mdx}',
        './src/app/**/*.{js,ts,jsx,tsx,mdx}',
    ],
    theme: {
        extend: {
            colors: {
                background: '#09090b',
                foreground: '#fafafa',
                primary: {
                    DEFAULT: '#10b981', // Emerald/Hexagon Green
                    foreground: '#000000',
                },
                card: {
                    DEFAULT: 'rgba(24, 24, 27, 0.4)', // Glassmorphism dark
                    border: 'rgba(255,255,255,0.08)'
                }
            },
            animation: {
                'pulse-slow': 'pulse 3s cubic-bezier(0.4, 0, 0.6, 1) infinite',
                'glow-pulse': 'glow-pulse 2s ease-in-out infinite alternate',
                'slide-up': 'slide-up 0.4s ease-out forwards',
                'fade-in': 'fade-in 0.3s ease-out forwards',
                'progress': 'progress 1.5s ease-in-out infinite',
            },
            keyframes: {
                'glow-pulse': {
                    '0%': { boxShadow: '0 0 15px -5px theme("colors.primary.DEFAULT")' },
                    '100%': { boxShadow: '0 0 40px 0px theme("colors.primary.DEFAULT")' },
                },
                'slide-up': {
                    '0%': { opacity: '0', transform: 'translateY(10px)' },
                    '100%': { opacity: '1', transform: 'translateY(0)' },
                },
                'fade-in': {
                    '0%': { opacity: '0' },
                    '100%': { opacity: '1' },
                },
                'progress': {
                    '0%': { backgroundPosition: '200% 0' },
                    '100%': { backgroundPosition: '-200% 0' },
                }
            }
        },
    },
    plugins: [],
}
export default config

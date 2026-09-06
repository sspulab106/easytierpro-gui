/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{vue,ts,js}'],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        // Brutalist neutral ramp: cement (light) -> ink (dark).
        // Surface-* keeps its role as the app's neutral scale so existing
        // `surface-*` / `dark:surface-*` utilities keep working.
        surface: {
          50: '#FAF9F4',
          100: '#E8E4D9', // cement gray
          200: '#D6D0C0',
          300: '#B8B2A2',
          400: '#9A947F',
          500: '#7A7565',
          600: '#5A5649',
          700: '#3D3A30',
          800: '#26241D',
          900: '#111111', // ink
          950: '#0A0A0A', // near-black (dark mode bg)
        },
        accent: {
          50: '#FFF1EC',
          100: '#FFE4DA',
          200: '#FFC9B3',
          300: '#FF8A66',
          400: '#FF5A2B',
          500: '#FF3B00', // signal orange
          600: '#E63500',
          700: '#C22E00',
          800: '#9E2600',
          950: '#4A1200',
        },
        blue: {
          DEFAULT: '#2438FF', // electric blue
          400: '#4A5AFF',
          500: '#2438FF',
          600: '#1E2FD9',
          700: '#1826B3',
        },
        success: { DEFAULT: '#00C200', 600: '#00A400' },
        warning: { DEFAULT: '#FFE600', 600: '#D9C600' }, // warning yellow
        danger: { DEFAULT: '#FF2E2E', 600: '#E01E1E' },
      },
      fontFamily: {
        sans: ['Inter', 'Microsoft YaHei UI', 'Noto Sans SC', 'system-ui', '-apple-system', 'sans-serif'],
        mono: ['"Courier New"', 'Courier', 'Consolas', '"Microsoft YaHei UI"', 'monospace'],
        display: ['"Arial Black"', 'Arial', '"Microsoft YaHei UI"', 'sans-serif'],
      },
    },
  },
  plugins: [],
}

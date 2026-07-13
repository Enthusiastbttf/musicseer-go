/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        base: '#070b12',
        panel: '#0d131d',
        accent: '#8b5cf6',
      },
    },
  },
  plugins: [],
}

/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        base: '#070b12',
        panel: '#0d131d',
        accent: '#2dd4bf',
        'accent-hover': '#5eead4',
        'accent-ink': '#04211e',
      },
    },
  },
  plugins: [],
}

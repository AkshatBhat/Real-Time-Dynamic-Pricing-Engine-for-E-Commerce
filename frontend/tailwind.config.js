/** @type {import("tailwindcss").Config} */
module.exports = {
  content: [
    "./app/**/*.{js,ts,jsx,tsx,mdx}",
    "./components/**/*.{js,ts,jsx,tsx,mdx}",
    "./lib/**/*.{js,ts,jsx,tsx,mdx}",
  ],
  theme: {
    extend: {
      colors: {
        "ink-950": "#07182b",
        "ink-900": "#0b2540",
        "ink-700": "#1f4b73",
        skyline: "#0ea5e9",
        mintline: "#14b8a6",
        signal: "#f59e0b",
        emberline: "#fb923c",
      },
      boxShadow: {
        panel: "0 18px 50px -24px rgba(7, 24, 43, 0.45)",
      },
      keyframes: {
        rise: {
          "0%": { opacity: "0", transform: "translateY(10px)" },
          "100%": { opacity: "1", transform: "translateY(0)" },
        },
      },
      animation: {
        rise: "rise 420ms ease-out forwards",
      },
    },
  },
  plugins: [],
};

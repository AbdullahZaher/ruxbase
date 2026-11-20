import "./styles.css";
export const metadata = { title: "Ruxbase · Next.js starter", description: "A local development starter using the Ruxbase SDK." };
export default function RootLayout({ children }) {
  return <html lang="en" data-theme="dark"><body>{children}</body></html>;
}

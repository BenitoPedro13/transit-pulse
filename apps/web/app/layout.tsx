import type { Metadata } from "next";
import { Archivo, Doto } from "next/font/google";
import "./globals.css";
import Providers from "./providers";

// Archivo is the body and UI face; its width axis gives the condensed cut used for small labels
// (font-stretch). Doto is the dot-matrix face of the letreiro, set at 20 px or larger only.
const archivo = Archivo({
  variable: "--font-archivo",
  subsets: ["latin", "latin-ext"],
  axes: ["wdth"],
});

const doto = Doto({
  variable: "--font-doto",
  subsets: ["latin", "latin-ext"],
  weight: "variable",
});

export const metadata: Metadata = {
  title: "Transit Pulse — Rio de Janeiro",
  description:
    "Mapa ao vivo do transporte público da Região Metropolitana do Rio de Janeiro, com dados reais dos operadores.",
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html lang="pt-BR" className={`${archivo.variable} ${doto.variable} h-full antialiased`}>
      <body className="min-h-full">
        <Providers>{children}</Providers>
      </body>
    </html>
  );
}

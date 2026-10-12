import { BRT_COLOR, BUS_COLOR, OUTLINE_COLOR } from "./vehicle-style";

// Static: the same shapes as the map, with their names in words.
export default function Legend() {
  return (
    <ul aria-label="Legenda do mapa" className="flex flex-wrap gap-x-6 gap-y-1 text-sm text-zinc-800">
      <li className="flex items-center gap-2">
        <svg aria-hidden="true" width="16" height="16" viewBox="0 0 16 16">
          <circle cx="8" cy="8" r="6" fill={BUS_COLOR} stroke={OUTLINE_COLOR} strokeWidth="1.5" />
        </svg>
        Círculo: ônibus
      </li>
      <li className="flex items-center gap-2">
        <svg aria-hidden="true" width="16" height="16" viewBox="0 0 16 16">
          <rect x="2" y="2" width="12" height="12" fill={BRT_COLOR} stroke={OUTLINE_COLOR} strokeWidth="1.5" />
        </svg>
        Quadrado: BRT
      </li>
    </ul>
  );
}

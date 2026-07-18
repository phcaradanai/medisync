// Spinner — loading indicator using UnoCSS utilities.
import { t } from "../i18n.ts";

export default function Spinner({ label }: { label?: string }) {
  return (
    <div className="flex flex-col items-center justify-center p-8 gap-3">
      <div
        role="status"
        aria-label={label || t("loading")}
        className="w-8 h-8 border-3 border-solid border-gray-200 border-t-blue-500 rounded-full animate-spin"
      />
      {label && <p className="text-gray-400 text-sm">{label}</p>}
    </div>
  );
}

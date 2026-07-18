// LanguageToggle — 🇹🇭🇬🇧 switch with UnoCSS.
import { toggleLanguage, getLanguage } from "../i18n.ts";
import { useState } from "react";

export default function LanguageToggle() {
  const [lang, setLang] = useState(getLanguage());

  const handleToggle = () => {
    const next = toggleLanguage();
    setLang(next);
    window.location.reload();
  };

  return (
    <button
      onClick={handleToggle}
      className="bg-gray-700 text-gray-200 border border-gray-500 rounded-md px-2.5 py-1 text-xs cursor-pointer transition-all duration-150 hover:bg-blue-500 hover:text-white"
      title={lang === "th" ? "Switch to English" : "เปลี่ยนเป็นภาษาไทย"}
    >
      {lang === "th" ? "🇹🇭 TH" : "🇬🇧 EN"}
    </button>
  );
}

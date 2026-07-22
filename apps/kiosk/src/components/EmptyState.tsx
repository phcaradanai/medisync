// EmptyState — UnoCSS-powered empty placeholder.
export default function EmptyState({ icon, message }: { icon: string; message: string }) {
  return (
    <div className="flex flex-col items-center justify-center py-12 px-4 gap-4 text-center">
      <div className="text-5xl opacity-50">{icon}</div>
      <p className="text-gray-400 text-base max-w-280px">{message}</p>
    </div>
  );
}

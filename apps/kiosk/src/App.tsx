// M1 scaffold. The real kiosk flows (withdraw, refill) are M4 and will be
// shaped with /impeccable before implementation. Keep this shell free of
// throwaway UI so M4 starts clean.
export default function App() {
  return (
    <main className="scaffold">
      <h1>MediSync — ตู้จ่ายยา</h1>
      <p>โครงระบบพร้อมแล้ว หน้าจอเบิกยา/เติมยาจะพัฒนาใน M4</p>
    </main>
  );
}

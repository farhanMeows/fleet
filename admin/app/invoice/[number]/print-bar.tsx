"use client";

export default function PrintBar() {
  return (
    <div className="inv-actions no-print">
      <button onClick={() => window.print()}>Download / Print PDF</button>
    </div>
  );
}

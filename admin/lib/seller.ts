// Seller identity printed on invoices.
export const SELLER = {
  name: "Fleetdeck",
  addressLines: ["Riverside, California", "United States"],
  email: "contact@fleetdeck.in",
  // GST registration for non-resident online service providers (rendered as
  // the "GSTIN:" line on invoices). Never put another company's tax id here.
  gstin: "9935USA34043OS5",
  // Shown on invoices only while gstin is blank.
  note: "",
};

// Non-resident OIDAR supplies into India are always inter-state, so IGST
// applies across the board — no CGST+SGST split needed.
export const GST_RATE = 0.18;

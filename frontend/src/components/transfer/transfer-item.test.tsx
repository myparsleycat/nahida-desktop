// @vitest-environment jsdom

import { UploadPhase } from "@bindings/transfer";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { TransferItemProps } from "./types";

import { TransferItem } from "./transfer-item";

vi.mock("@bindings/platform", () => ({ Shell: { OpenPath: vi.fn() } }));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string) => key }) }));

afterEach(cleanup);

const props: TransferItemProps = {
  id: "upload",
  fileName: "file.bin",
  fileSize: "1 MiB",
  fileType: "file",
  progress: 50,
  speed: "1 MiB/s",
  timeRemaining: "1s",
  status: "uploading",
  type: "upload",
};

describe("TransferItem upload activity", () => {
  it("shows response waiting with the actual percentage and hides stale metrics", () => {
    render(<TransferItem {...props} uploadPhase={UploadPhase.UploadWaiting} />);
    expect(screen.getByText("page.transfer.item.upload_activity.waiting")).toBeDefined();
    expect(screen.getByText("50.00%")).toBeDefined();
    expect(screen.queryByText("1 MiB/s")).toBeNull();
  });

  it("shows file preparation and restores speed when sending resumes", () => {
    const view = render(<TransferItem {...props} uploadPhase={UploadPhase.UploadPreparing} />);
    expect(screen.getByText("page.transfer.item.upload_activity.preparing")).toBeDefined();
    view.rerender(<TransferItem {...props} uploadPhase={UploadPhase.UploadTransferring} />);
    expect(screen.getByText("1 MiB/s")).toBeDefined();
  });

  it("keeps finalizing visible after all bytes have been sent", () => {
    render(<TransferItem {...props} progress={100} uploadPhase={UploadPhase.UploadWaiting} />);
    expect(screen.getByText("page.transfer.item.finalizing")).toBeDefined();
    expect(screen.queryByText("page.transfer.item.upload_activity.waiting")).toBeNull();
  });
});

// @vitest-environment jsdom

import { cleanup, render } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { Markdown } from "./markdown";

afterEach(cleanup);

describe("Markdown", () => {
  it("keeps blank-line separated paragraphs apart and single newlines visible", () => {
    const { container } = render(
      <Markdown text={"첫 번째 문단입니다.\n같은 문단의 둘째 줄입니다.\n\n새 문단입니다."} />,
    );

    const paragraphs = container.querySelectorAll(".markdown-body > p");
    expect(paragraphs).toHaveLength(2);
    expect(paragraphs[0]?.querySelector("br")).not.toBeNull();
    expect(paragraphs[0]?.textContent).toBe("첫 번째 문단입니다.\n같은 문단의 둘째 줄입니다.");
    expect(paragraphs[1]?.textContent).toBe("새 문단입니다.");
  });

  it("renders headings, lists, and tables with structure", () => {
    const { container } = render(
      <Markdown
        text={
          "## 설치 순서\n\n- 첫 단계\n- 둘째 단계\n\n| 항목 | 값 |\n| --- | --- |\n| 경로 | ini |"
        }
      />,
    );

    expect(container.querySelector("h2")?.textContent).toBe("설치 순서");
    expect(container.querySelectorAll("ul > li")).toHaveLength(2);
    expect(container.querySelectorAll("table td")).toHaveLength(2);
  });

  it("keeps fenced code blocks intact", () => {
    const { container } = render(<Markdown text={"```ini\nKey=Value\n```"} />);

    expect(container.querySelector("pre code")?.textContent).toContain("Key=Value");
  });

  it("shows the streaming caret only while a run is active", () => {
    const { container, rerender } = render(<Markdown text="응답 중" streaming />);
    expect(container.querySelector(".markdown-caret")).not.toBeNull();

    rerender(<Markdown text="응답 중" />);
    expect(container.querySelector(".markdown-caret")).toBeNull();
  });
});

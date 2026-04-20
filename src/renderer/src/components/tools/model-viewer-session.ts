export function base64GlbToObjectUrl(glbBase64: string): string {
  const binary = atob(glbBase64);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i += 1) {
    bytes[i] = binary.charCodeAt(i);
  }

  return URL.createObjectURL(new Blob([bytes as BlobPart], { type: "model/gltf-binary" }));
}

export function suppressModelViewerFocusOutline(element: HTMLElement | null): void {
  if (!element) {
    return;
  }

  requestAnimationFrame(() => {
    const shadowRoot = element.shadowRoot;
    if (!shadowRoot || shadowRoot.querySelector("style[data-nhd-focus-outline]")) {
      return;
    }

    const style = document.createElement("style");
    style.dataset.nhdFocusOutline = "true";
    style.textContent = ".userInput { outline: none !important; }";
    shadowRoot.appendChild(style);
  });
}

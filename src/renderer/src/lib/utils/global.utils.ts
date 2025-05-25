export function clickWithoutDrag(node, callback) {
    let startX = 0;
    let startY = 0;
    let isDragging = false;
    let mouseDownTarget = null;
    const DRAG_THRESHOLD = 5;

    function handleMouseDown(e) {
        startX = e.clientX;
        startY = e.clientY;
        isDragging = false;
        mouseDownTarget = e.target;
    }

    function handleMouseMove(e) {
        if (Math.abs(e.clientX - startX) > DRAG_THRESHOLD ||
            Math.abs(e.clientY - startY) > DRAG_THRESHOLD) {
            isDragging = true;
        }
    }

    function handleClick(e) {
        if (isDragging) {
            e.preventDefault();
            e.stopPropagation();
            return;
        }

        if (e.defaultPrevented || e.target !== mouseDownTarget) {
            return;
        }

        if (e.target !== node && e.target !== mouseDownTarget) {
            return;
        }

        if (callback && e.target === node) {
            callback(e);
        }
    }

    node.addEventListener('mousedown', handleMouseDown, { capture: true });
    node.addEventListener('mousemove', handleMouseMove);
    node.addEventListener('click', handleClick, { capture: false });

    return {
        update(newCallback) {
            callback = newCallback;
        },
        destroy() {
            node.removeEventListener('mousedown', handleMouseDown, { capture: true });
            node.removeEventListener('mousemove', handleMouseMove);
            node.removeEventListener('click', handleClick, { capture: false });
        }
    };
}
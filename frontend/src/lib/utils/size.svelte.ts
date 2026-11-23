// A Svelte store that tracks the size of an HTML element using ResizeObserver.
export function useElementSize() {
    let width = $state(0);
    let height = $state(0);

    const observe = (node: HTMLElement) => {
        const observer = new ResizeObserver((entries) => {
            if (entries[0]) {
                width = entries[0].contentRect.width;
                height = entries[0].contentRect.height;
            }
        });
        observer.observe(node);

        return () => {
            observer.disconnect();
        };
    };
    return {
        get width() {
            return width;
        },
        get height() {
            return height;
        },
        observe
    }
}

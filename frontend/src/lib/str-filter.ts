export const naturalCompare = (a: string, b: string, mp: number) => {
    const collator = new Intl.Collator(undefined, {
        numeric: true,
        sensitivity: "case",
        caseFirst: "lower",
    });
    return mp * collator.compare(a, b);
};

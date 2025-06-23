<script lang="ts">
	import type {
		HTMLInputAttributes,
		HTMLInputTypeAttribute,
	} from "svelte/elements";
	import { cn, type WithElementRef } from "$lib/utils.js";

	type InputType = Exclude<HTMLInputTypeAttribute, "file">;

	type Props = WithElementRef<
		Omit<HTMLInputAttributes, "type"> &
			(
				| { type: "file"; files?: FileList }
				| { type?: InputType; files?: undefined }
			)
	>;

	let {
		ref = $bindable(null),
		value = $bindable(),
		type,
		files = $bindable(),
		class: className,
		min,
		max,
		...restProps
	}: Props = $props();

	// Handle input validation for number type
	function handleInput(event: Event) {
		const target = event.target as HTMLInputElement;

		// Only apply validation for number type
		if (type === "number" && target.value !== "") {
			let numValue = parseFloat(target.value);

			// Check if the parsed value is a valid number
			if (!isNaN(numValue)) {
				// Apply min constraint
				if (min && numValue < parseFloat(min.toString())) {
					numValue = parseFloat(min.toString());
					target.value = numValue.toString();
					value = target.value;
				}
				// Apply max constraint
				else if (max && numValue > parseFloat(max.toString())) {
					numValue = parseFloat(max.toString());
					target.value = numValue.toString();
					value = target.value;
				}
			}
		}
	}

	// Handle blur event to ensure final validation
	function handleBlur(event: Event) {
		const target = event.target as HTMLInputElement;

		// Apply same validation logic on blur
		if (type === "number" && target.value !== "") {
			let numValue = parseFloat(target.value);

			// Check if the parsed value is a valid number
			if (!isNaN(numValue)) {
				// Apply min constraint
				if (min && numValue < parseFloat(min.toString())) {
					numValue = parseFloat(min.toString());
					target.value = numValue.toString();
					value = target.value;
				}
				// Apply max constraint
				else if (max && numValue > parseFloat(max.toString())) {
					numValue = parseFloat(max.toString());
					target.value = numValue.toString();
					value = target.value;
				}
			}
		}
	}
</script>

{#if type === "file"}
	<input
		bind:this={ref}
		data-slot="input"
		class={cn(
			"selection:bg-primary dark:bg-input/30 selection:text-primary-foreground border-input ring-offset-background placeholder:text-muted-foreground shadow-xs flex h-9 w-full min-w-0 rounded-md border bg-transparent px-3 py-2 text-sm font-medium outline-none transition-[color,box-shadow] disabled:cursor-not-allowed disabled:opacity-50 md:text-sm",
			"focus-visible:border-ring focus-visible:ring-ring/50 focus-visible:ring-[3px]",
			"aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40 aria-invalid:border-destructive",
			className,
		)}
		type="file"
		bind:files
		bind:value
		{...restProps}
	/>
{:else}
	<input
		bind:this={ref}
		data-slot="input"
		class={cn(
			"border-input bg-background selection:bg-primary dark:bg-input/30 selection:text-primary-foreground ring-offset-background placeholder:text-muted-foreground shadow-xs flex h-9 w-full min-w-0 rounded-md border px-3 py-1 text-base outline-none transition-[color,box-shadow] disabled:cursor-not-allowed disabled:opacity-50 md:text-sm",
			"focus-visible:border-ring focus-visible:ring-ring/50 focus-visible:ring-[3px]",
			"aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40 aria-invalid:border-destructive",
			className,
		)}
		{type}
		{min}
		{max}
		bind:value
		oninput={handleInput}
		onblur={handleBlur}
		{...restProps}
	/>
{/if}

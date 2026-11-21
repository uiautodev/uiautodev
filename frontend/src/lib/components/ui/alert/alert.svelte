<script lang="ts" module>
	import { type VariantProps, tv } from "tailwind-variants";

	export const alertVariants = tv({
		base: "relative grid w-full grid-cols-[0_1fr] items-start gap-y-0.5 rounded-lg border px-4 py-3 text-sm has-[>svg]:grid-cols-[calc(var(--spacing)*4)_1fr] has-[>svg]:gap-x-3 [&>svg]:size-4 [&>svg]:translate-y-0.5 [&>svg]:text-current",
		variants: {
			variant: {
				default: "bg-card text-card-foreground",
				destructive:
					"text-destructive bg-card *:data-[slot=alert-description]:text-destructive/90 [&>svg]:text-current",
				info: "border-blue-300 bg-blue-50 text-blue-900 *:data-[slot=alert-description]:text-blue-700/90 [&>svg]:text-current",
				success:
					"border-emerald-300 bg-emerald-50 text-emerald-900 *:data-[slot=alert-description]:text-emerald-700/90 [&>svg]:text-current",
				warning:
					"border-amber-300 bg-amber-50 text-amber-900 *:data-[slot=alert-description]:text-amber-700/90 [&>svg]:text-current",
				error: "border-red-300 bg-red-50 text-red-900 *:data-[slot=alert-description]:text-red-700/90 [&>svg]:text-current",
			},
		},
		defaultVariants: {
			variant: "default",
		},
	});

	export type AlertVariant = VariantProps<typeof alertVariants>["variant"];
</script>

<script lang="ts">
	import type { HTMLAttributes } from "svelte/elements";
	import { cn, type WithElementRef } from "$lib/utils.js";

	let {
		ref = $bindable(null),
		class: className,
		variant = "default",
		children,
		...restProps
	}: WithElementRef<HTMLAttributes<HTMLDivElement>> & {
		variant?: AlertVariant;
	} = $props();
</script>

<div
	bind:this={ref}
	data-slot="alert"
	class={cn(alertVariants({ variant }), className)}
	{...restProps}
	role="alert"
>
	{@render children?.()}
</div>

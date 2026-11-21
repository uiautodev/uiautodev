<script lang="ts">
	import FileIcon from '@lucide/svelte/icons/file';
	import { cn } from '$lib/utils.js';
	import type { TreeViewFileProps } from './types';

	let { name, children, type = 'button', class: className, ...rest }: TreeViewFileProps = $props();
</script>

<button {type} class={cn('flex place-items-center gap-1 pl-[3px]', className)} {...rest}>
	{#if children}
		{@render children?.()}
	{:else}
		<FileIcon class="size-4" />
		<span>{name}</span>
	{/if}
</button>

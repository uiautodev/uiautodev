<script lang="ts">
	import * as Collapsible from '$lib/components/ui/collapsible/index.js';
	import FolderIcon from '@lucide/svelte/icons/folder';
	import FolderOpenIcon from '@lucide/svelte/icons/folder-open';
	import { cn } from '$lib/utils.js';
	import type { TreeViewFolderProps } from './types';

	let {
		name,
		open = $bindable(true),
		class: className,
		icon,
		children
	}: TreeViewFolderProps = $props();
</script>

<Collapsible.Root bind:open>
	
	<Collapsible.Content class="mx-2 border-l">
		<div class="relative flex place-items-start">
			<div class="bg-border mx-2 h-full w-px"></div>
			<div class="flex flex-col">
				{@render children?.()}
			</div>
		</div>
	</Collapsible.Content>
</Collapsible.Root>

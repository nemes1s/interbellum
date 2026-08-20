"use client";

import {
  Background,
  BackgroundVariant,
  Controls,
  MarkerType,
  ReactFlow,
  ReactFlowProvider,
  useReactFlow,
  type Edge,
  type Node,
} from "@xyflow/react";
import { useEffect, useMemo } from "react";

import { layoutGraph } from "@/lib/graph-layout";
import type { PathOverlay } from "@/lib/report-path";
import type { PlaybookEdge, PlaybookNode, UUID } from "@/lib/types";

import { graphEdgeTypes, type GraphEdgeData } from "./edges";
import { graphNodeTypes, type GraphNodeData } from "./nodes";

export interface PlaybookGraphProps {
  nodes: PlaybookNode[];
  edges: PlaybookEdge[];
  rootNodeId: UUID | null;
  /**
   * Path overlay to draw on top of the canonical graph. When absent the graph
   * is drawn plainly — the same nodes and edges, nothing dimmed.
   */
  overlay?: PathOverlay;
  onNodeClick?: (nodeId: UUID) => void;
  selectedNodeId?: UUID | null;
  className?: string;
}

function PlaybookGraphInner({
  nodes,
  edges,
  rootNodeId,
  overlay,
  onNodeClick,
  selectedNodeId,
}: PlaybookGraphProps) {
  const { flowNodes, flowEdges } = useMemo(() => {
    const layout = layoutGraph(nodes, edges);
    const overlaid = overlay !== undefined && overlay.visited.size > 0;

    const flowNodes: Node<GraphNodeData>[] = nodes.map((node) => {
      const box = layout.nodes.get(node.id);
      const isVisited = overlay?.visited.has(node.id) ?? false;
      return {
        id: node.id,
        type: node.kind,
        position: { x: box?.x ?? 0, y: box?.y ?? 0 },
        // Declared rather than measured from the DOM, so the first `fitView`
        // knows the real extent of the graph instead of fitting to nodes it has
        // not laid out yet — otherwise a wide playbook opens cropped.
        initialWidth: box?.width,
        initialHeight: box?.height,
        draggable: false,
        selectable: Boolean(onNodeClick),
        selected: selectedNodeId === node.id,
        data: {
          kind: node.kind,
          title: node.title,
          description: node.description,
          resolution: node.terminal_resolution,
          isRoot: node.id === rootNodeId,
          isVisited,
          isFinal: overlay?.finalNodeId === node.id,
          stepNumber: overlay?.stepAtNode.get(node.id) ?? null,
          isGhost: overlaid && !isVisited,
        },
      };
    });

    const flowEdges: Edge<GraphEdgeData>[] = edges.map((edge) => {
      const isSelected = overlay?.selectedEdgeIds.has(edge.id) ?? false;
      return {
        id: edge.id,
        source: edge.from_node_id,
        target: edge.to_node_id,
        type: "schematic",
        // A taken edge draws above the ghosts.
        zIndex: isSelected ? 2 : 1,
        markerEnd: {
          type: MarkerType.ArrowClosed,
          width: 14,
          height: 14,
          color: isSelected
            ? "var(--color-signal)"
            : overlaid
              ? "var(--color-rule)"
              : "var(--color-rule-strong)",
        },
        data: {
          label: edge.label,
          isSelected,
          stepNumber: overlay?.stepAtEdge.get(edge.id) ?? null,
          isGhost: overlaid && !isSelected,
        },
      };
    });

    return { flowNodes, flowEdges };
  }, [nodes, edges, rootNodeId, overlay, onNodeClick, selectedNodeId]);

  // Re-fit when the graph itself changes — switching playbook version swaps
  // every node, and the viewport from the previous graph would be meaningless.
  const { fitView } = useReactFlow();
  const graphKey = useMemo(() => nodes.map((node) => node.id).join(","), [nodes]);
  useEffect(() => {
    const frame = requestAnimationFrame(() => void fitView({ padding: 0.12, maxZoom: 1 }));
    return () => cancelAnimationFrame(frame);
  }, [graphKey, fitView]);

  return (
    <ReactFlow
      nodes={flowNodes}
      edges={flowEdges}
      nodeTypes={graphNodeTypes}
      edgeTypes={graphEdgeTypes}
      // The stored graph carries no coordinates and this is a viewer: nothing
      // here edits the playbook, so every mutation affordance is off.
      nodesDraggable={false}
      nodesConnectable={false}
      edgesFocusable={false}
      elementsSelectable={Boolean(onNodeClick)}
      onNodeClick={onNodeClick ? (_, node) => onNodeClick(node.id) : undefined}
      fitView
      fitViewOptions={{ padding: 0.12, maxZoom: 1 }}
      minZoom={0.15}
      maxZoom={1.6}
      proOptions={{ hideAttribution: true }}
    >
      <Background variant={BackgroundVariant.Dots} gap={16} size={1} color="var(--color-rule)" />
      <Controls showInteractive={false} position="bottom-right" />
    </ReactFlow>
  );
}

/**
 * Renders a playbook version's graph, optionally with an investigation path
 * overlaid.
 *
 * The graph passed in is the canonical one from the API. This component never
 * changes it — the overlay is a separate argument, and every highlight decision
 * is a lookup into it.
 */
export function PlaybookGraph({ className = "", ...props }: PlaybookGraphProps) {
  if (props.nodes.length === 0) {
    return (
      <div className={`flex items-center justify-center bg-sunken ${className}`}>
        <p className="label">This version has no nodes yet</p>
      </div>
    );
  }

  return (
    <div className={className}>
      <ReactFlowProvider>
        <PlaybookGraphInner {...props} />
      </ReactFlowProvider>
    </div>
  );
}

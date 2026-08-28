import { useState, useMemo } from "react";

import { TransferItemProps, TransferStatus, TransferTabType } from "../types";

interface UseTransferFilterProps {
    transfers: TransferItemProps[];
}

export function useTransferFilter({ transfers }: UseTransferFilterProps) {
    const [searchQuery, setSearchQuery] = useState("");
    const [activeTab, setActiveTab] = useState<TransferTabType>("all");
    const [statusFilter, setStatusFilter] = useState<TransferStatus[]>([]);

    const filteredTransfers = useMemo(() => {
        return transfers.filter((transfer) => {
            const matchesSearch = transfer.fileName
                .toLowerCase()
                .includes(searchQuery.toLowerCase());
            const matchesTab =
                activeTab === "all" ||
                (activeTab === "uploads" && transfer.type === "upload") ||
                (activeTab === "downloads" && transfer.type === "download");
            const matchesStatus =
                statusFilter.length === 0 || statusFilter.includes(transfer.status);
            return matchesSearch && matchesTab && matchesStatus;
        });
    }, [transfers, searchQuery, activeTab, statusFilter]);

    const counts = useMemo(() => {
        return {
            total: transfers.length,
            uploads: transfers.filter((t) => t.type === "upload").length,
            downloads: transfers.filter((t) => t.type === "download").length,
            active: transfers.filter((t) => t.status === "uploading" || t.status === "downloading")
                .length,
            completed: transfers.filter((t) => t.status === "completed").length,
            paused: transfers.filter((t) => t.status === "paused").length,
        };
    }, [transfers]);

    const toggleStatusFilter = (status: TransferStatus) => {
        setStatusFilter((prev) =>
            prev.includes(status) ? prev.filter((s) => s !== status) : [...prev, status],
        );
    };

    return {
        searchQuery,
        setSearchQuery,
        activeTab,
        setActiveTab,
        statusFilter,
        toggleStatusFilter,
        filteredTransfers,
        counts,
    };
}

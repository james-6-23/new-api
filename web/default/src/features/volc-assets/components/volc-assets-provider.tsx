/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import React, { useState } from 'react'
import useDialogState from '@/hooks/use-dialog'
import { type VolcAsset } from '../types'

type VolcAssetsDialogType = 'delete'

interface VolcAssetsContextType {
  open: VolcAssetsDialogType | null
  setOpen: (open: VolcAssetsDialogType | null) => void
  currentRow: VolcAsset | null
  setCurrentRow: React.Dispatch<React.SetStateAction<VolcAsset | null>>
  refreshTrigger: number
  triggerRefresh: () => void
}

const VolcAssetsContext = React.createContext<VolcAssetsContextType | null>(
  null
)

export function VolcAssetsProvider({
  children,
}: {
  children: React.ReactNode
}) {
  const [open, setOpen] = useDialogState<VolcAssetsDialogType>(null)
  const [currentRow, setCurrentRow] = useState<VolcAsset | null>(null)
  const [refreshTrigger, setRefreshTrigger] = useState(0)

  const triggerRefresh = () => setRefreshTrigger((prev) => prev + 1)

  return (
    <VolcAssetsContext
      value={{
        open,
        setOpen,
        currentRow,
        setCurrentRow,
        refreshTrigger,
        triggerRefresh,
      }}
    >
      {children}
    </VolcAssetsContext>
  )
}

export const useVolcAssets = () => {
  const context = React.useContext(VolcAssetsContext)
  if (!context) {
    throw new Error('useVolcAssets has to be used within <VolcAssetsProvider>')
  }
  return context
}

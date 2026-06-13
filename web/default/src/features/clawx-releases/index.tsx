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
import { FormEvent, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus, RefreshCw, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { formatTimestampToDate } from '@/lib/format'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Switch } from '@/components/ui/switch'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Textarea } from '@/components/ui/textarea'
import { SectionPageLayout } from '@/components/layout'
import { StatusBadge } from '@/components/status-badge'
import {
  createClawXRelease,
  deleteClawXRelease,
  getClawXReleases,
  updateClawXRelease,
} from './api'
import type {
  ClawXRelease,
  ClawXReleaseFormData,
  ClawXReleasePlatform,
} from './types'

const DEFAULT_RELEASE: ClawXReleaseFormData = {
  channel: 'latest',
  platform: 'mac',
  arch: 'arm64',
  version: '',
  file_name: '',
  file_url: '',
  sha512: '',
  size: 0,
  release_date: new Date().toISOString(),
  release_notes: '',
  enabled: true,
  mandatory: false,
}

function bytesToSize(size: number) {
  if (!Number.isFinite(size) || size <= 0) return '-'
  const mb = size / 1024 / 1024
  return `${mb.toFixed(mb >= 100 ? 0 : 1)} MB`
}

function releaseToForm(release: ClawXRelease): ClawXReleaseFormData {
  return {
    channel: release.channel,
    platform: release.platform,
    arch: release.arch,
    version: release.version,
    file_name: release.file_name,
    file_url: release.file_url,
    sha512: release.sha512,
    size: release.size,
    release_date: release.release_date,
    release_notes: release.release_notes,
    enabled: release.enabled,
    mandatory: release.mandatory,
  }
}

export function ClawXReleases() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [editing, setEditing] = useState<ClawXRelease | null>(null)
  const [form, setForm] = useState<ClawXReleaseFormData>(DEFAULT_RELEASE)
  const [dialogOpen, setDialogOpen] = useState(false)

  const query = useQuery({
    queryKey: ['clawx-releases'],
    queryFn: getClawXReleases,
  })

  const releases = useMemo(
    () => query.data?.data?.items || [],
    [query.data?.data?.items]
  )

  const saveMutation = useMutation({
    mutationFn: async (payload: ClawXReleaseFormData) => {
      return editing
        ? updateClawXRelease(editing.id, payload)
        : createClawXRelease(payload)
    },
    onSuccess: (result) => {
      if (!result.success) return
      toast.success(t('Saved successfully'))
      setDialogOpen(false)
      setEditing(null)
      void queryClient.invalidateQueries({ queryKey: ['clawx-releases'] })
    },
  })

  const deleteMutation = useMutation({
    mutationFn: deleteClawXRelease,
    onSuccess: (result) => {
      if (!result.success) return
      toast.success(t('Deleted successfully'))
      void queryClient.invalidateQueries({ queryKey: ['clawx-releases'] })
    },
  })

  const openCreate = () => {
    setEditing(null)
    setForm({ ...DEFAULT_RELEASE, release_date: new Date().toISOString() })
    setDialogOpen(true)
  }

  const openEdit = (release: ClawXRelease) => {
    setEditing(release)
    setForm(releaseToForm(release))
    setDialogOpen(true)
  }

  const updateForm = <K extends keyof ClawXReleaseFormData>(
    key: K,
    value: ClawXReleaseFormData[K]
  ) => {
    setForm((current) => ({ ...current, [key]: value }))
  }

  const onSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    saveMutation.mutate(form)
  }

  return (
    <>
      <SectionPageLayout fixedContent>
        <SectionPageLayout.Title>{t('ClawX Versions')}</SectionPageLayout.Title>
        <SectionPageLayout.Actions>
          <Button
            variant='outline'
            onClick={() =>
              void queryClient.invalidateQueries({
                queryKey: ['clawx-releases'],
              })
            }
          >
            <RefreshCw />
            {t('Refresh')}
          </Button>
          <Button onClick={openCreate}>
            <Plus />
            {t('New Version')}
          </Button>
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <div className='h-full overflow-hidden rounded-lg border'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('Channel')}</TableHead>
                  <TableHead>{t('Platform')}</TableHead>
                  <TableHead>{t('Version')}</TableHead>
                  <TableHead>{t('File')}</TableHead>
                  <TableHead>{t('Size')}</TableHead>
                  <TableHead>{t('Status')}</TableHead>
                  <TableHead>{t('Updated')}</TableHead>
                  <TableHead className='w-[140px]'>{t('Actions')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {query.isLoading ? (
                  <TableRow>
                    <TableCell colSpan={8}>{t('Loading...')}</TableCell>
                  </TableRow>
                ) : releases.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={8}>
                      {t('No ClawX versions found')}
                    </TableCell>
                  </TableRow>
                ) : (
                  releases.map((release) => (
                    <TableRow key={release.id}>
                      <TableCell>{release.channel}</TableCell>
                      <TableCell>
                        {release.platform} / {release.arch}
                      </TableCell>
                      <TableCell className='font-mono'>
                        {release.version}
                      </TableCell>
                      <TableCell>
                        <div className='max-w-[320px] truncate'>
                          {release.file_name || release.file_url}
                        </div>
                      </TableCell>
                      <TableCell>{bytesToSize(release.size)}</TableCell>
                      <TableCell>
                        <StatusBadge
                          label={release.enabled ? t('Enabled') : t('Disabled')}
                          variant={release.enabled ? 'success' : 'neutral'}
                          copyable={false}
                        />
                      </TableCell>
                      <TableCell>
                        {release.updated_at
                          ? formatTimestampToDate(release.updated_at)
                          : '-'}
                      </TableCell>
                      <TableCell>
                        <div className='flex items-center gap-2'>
                          <Button
                            size='sm'
                            variant='outline'
                            onClick={() => openEdit(release)}
                          >
                            {t('Edit')}
                          </Button>
                          <Button
                            size='icon-sm'
                            variant='ghost'
                            onClick={() => deleteMutation.mutate(release.id)}
                          >
                            <Trash2 />
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className='sm:max-w-2xl'>
          <DialogHeader>
            <DialogTitle>
              {editing ? t('Edit ClawX Version') : t('New ClawX Version')}
            </DialogTitle>
          </DialogHeader>
          <form id='clawx-release-form' onSubmit={onSubmit}>
            <div className='grid gap-3 sm:grid-cols-2'>
              <div className='grid gap-1.5'>
                <Label>{t('Channel')}</Label>
                <Input
                  value={form.channel}
                  onChange={(e) => updateForm('channel', e.target.value)}
                />
              </div>
              <div className='grid gap-1.5'>
                <Label>{t('Platform')}</Label>
                <NativeSelect
                  className='w-full'
                  value={form.platform}
                  onChange={(e) =>
                    updateForm(
                      'platform',
                      e.target.value as ClawXReleasePlatform
                    )
                  }
                >
                  <NativeSelectOption value='mac'>mac</NativeSelectOption>
                  <NativeSelectOption value='win'>win</NativeSelectOption>
                  <NativeSelectOption value='linux'>linux</NativeSelectOption>
                </NativeSelect>
              </div>
              <div className='grid gap-1.5'>
                <Label>{t('Arch')}</Label>
                <Input
                  value={form.arch}
                  onChange={(e) => updateForm('arch', e.target.value)}
                />
              </div>
              <div className='grid gap-1.5'>
                <Label>{t('Version')}</Label>
                <Input
                  value={form.version}
                  onChange={(e) => updateForm('version', e.target.value)}
                />
              </div>
              <div className='grid gap-1.5 sm:col-span-2'>
                <Label>{t('Download URL')}</Label>
                <Input
                  value={form.file_url}
                  onChange={(e) => updateForm('file_url', e.target.value)}
                />
              </div>
              <div className='grid gap-1.5'>
                <Label>{t('File Name')}</Label>
                <Input
                  value={form.file_name}
                  onChange={(e) => updateForm('file_name', e.target.value)}
                />
              </div>
              <div className='grid gap-1.5'>
                <Label>{t('Size')}</Label>
                <Input
                  type='number'
                  value={form.size || ''}
                  onChange={(e) =>
                    updateForm('size', Number(e.target.value) || 0)
                  }
                />
              </div>
              <div className='grid gap-1.5 sm:col-span-2'>
                <Label>sha512</Label>
                <Input
                  value={form.sha512}
                  onChange={(e) => updateForm('sha512', e.target.value)}
                />
              </div>
              <div className='grid gap-1.5 sm:col-span-2'>
                <Label>{t('Release Date')}</Label>
                <Input
                  value={form.release_date}
                  onChange={(e) => updateForm('release_date', e.target.value)}
                />
              </div>
              <div className='grid gap-1.5 sm:col-span-2'>
                <Label>{t('Release Notes')}</Label>
                <Textarea
                  value={form.release_notes}
                  onChange={(e) => updateForm('release_notes', e.target.value)}
                />
              </div>
              <label className='flex items-center gap-2 text-sm'>
                <Switch
                  checked={form.enabled}
                  onCheckedChange={(checked) =>
                    updateForm('enabled', Boolean(checked))
                  }
                />
                {t('Enabled')}
              </label>
              <label className='flex items-center gap-2 text-sm'>
                <Switch
                  checked={form.mandatory}
                  onCheckedChange={(checked) =>
                    updateForm('mandatory', Boolean(checked))
                  }
                />
                {t('Mandatory')}
              </label>
            </div>
          </form>
          <DialogFooter>
            <Button variant='outline' onClick={() => setDialogOpen(false)}>
              {t('Cancel')}
            </Button>
            <Button
              type='submit'
              form='clawx-release-form'
              disabled={saveMutation.isPending}
            >
              {saveMutation.isPending ? t('Saving...') : t('Save')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}

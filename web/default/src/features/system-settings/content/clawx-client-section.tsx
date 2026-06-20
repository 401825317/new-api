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
import { useMemo, useRef, useState, type ChangeEvent } from 'react'
import * as z from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { Edit, ImageUp, Loader2, Plus, Save, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import dayjs from '@/lib/dayjs'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { StaticDataTable } from '@/components/data-table'
import { DateTimePicker } from '@/components/datetime-picker'
import { Dialog } from '@/components/dialog'
import { StatusBadge } from '@/components/status-badge'
import { uploadClawXSupportQRCode } from '../api'
import { SettingsSwitchField } from '../components/settings-form-layout'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'

type ClientAnnouncement = {
  id: string
  title: string
  content: string
  level: 'normal' | 'important' | 'urgent'
  publishedAt: string
  expiresAt?: string
  link?: string
  enabled: boolean
}

type SupportConfig = {
  title: string
  description: string
  contacts: SupportContact[]
  qrCodeUrl?: string
  workHours?: string
  wechatId?: string
  extraNote?: string
}

type SupportContact = {
  id: string
  label: string
  description: string
  qrCodeUrl: string
  workHours: string
  wechatId: string
  extraNote: string
  enabled: boolean
}

type ClawXClientSectionProps = {
  announcementsEnabled: boolean
  announcementsData: string
  supportEnabled: boolean
  supportData: string
}

const announcementSchema = z.object({
  title: z.string().min(1, 'Title is required').max(80),
  content: z.string().min(1, 'Content is required').max(1000),
  level: z.enum(['normal', 'important', 'urgent']),
  publishedAt: z.string().min(1, 'Publish date is required'),
  expiresAt: z.string().optional(),
  link: z.string().url().or(z.literal('')).optional(),
  enabled: z.boolean(),
})

const supportSchema = z.object({
  title: z.string().max(60),
  description: z.string().max(300),
})

const supportContactSchema = z.object({
  label: z.string().min(1, 'Name is required').max(60),
  description: z.string().max(200),
  qrCodeUrl: z.string().url(),
  workHours: z.string().max(100),
  wechatId: z.string().max(100),
  extraNote: z.string().max(200),
  enabled: z.boolean(),
})

type AnnouncementFormValues = z.infer<typeof announcementSchema>
type SupportFormValues = z.infer<typeof supportSchema>
type SupportContactFormValues = z.infer<typeof supportContactSchema>

const ANNOUNCEMENT_FORM_ID = 'clawx-client-announcement-form'
const SUPPORT_FORM_ID = 'clawx-client-support-form'
const SUPPORT_CONTACT_FORM_ID = 'clawx-client-support-contact-form'
const SUPPORT_QRCODE_MAX_SIZE = 2 * 1024 * 1024
const SUPPORT_QRCODE_ACCEPT = 'image/png,image/jpeg,image/gif'

const levelOptions = [
  { value: 'normal', label: 'Normal', badgeVariant: 'neutral' as const },
  { value: 'important', label: 'Important', badgeVariant: 'warning' as const },
  { value: 'urgent', label: 'Urgent', badgeVariant: 'danger' as const },
]

function makeAnnouncementId(): string {
  return `client-${Date.now().toString(36)}`
}

function makeSupportContactId(): string {
  return `support-${Date.now().toString(36)}`
}

function parseAnnouncements(data: string): ClientAnnouncement[] {
  try {
    const parsed = JSON.parse(data || '[]')
    if (!Array.isArray(parsed)) {
      return []
    }
    return parsed.map((item, index) => ({
      id: String(item.id || `client-${index + 1}`),
      title: String(item.title || ''),
      content: String(item.content || ''),
      level: ['normal', 'important', 'urgent'].includes(item.level)
        ? item.level
        : 'normal',
      publishedAt: String(item.publishedAt || new Date().toISOString()),
      expiresAt: typeof item.expiresAt === 'string' ? item.expiresAt : '',
      link: typeof item.link === 'string' ? item.link : '',
      enabled: item.enabled !== false,
    }))
  } catch {
    return []
  }
}

function parseSupport(data: string): SupportConfig {
  try {
    const parsed = JSON.parse(data || '{}')
    const contacts = parseSupportContacts(parsed)
    return {
      title: typeof parsed.title === 'string' ? parsed.title : '',
      description:
        typeof parsed.description === 'string' ? parsed.description : '',
      contacts,
    }
  } catch {
    return {
      title: '',
      description: '',
      contacts: [],
    }
  }
}

function parseSupportContacts(
  parsed: Record<string, unknown>
): SupportContact[] {
  const rawContacts = Array.isArray(parsed.contacts) ? parsed.contacts : []
  const contacts = rawContacts
    .map((item, index) => {
      if (!item || typeof item !== 'object') {
        return null
      }
      const contact = item as Partial<SupportContact>
      return {
        id: String(contact.id || `support-${index + 1}`),
        label: String(contact.label || ''),
        description:
          typeof contact.description === 'string' ? contact.description : '',
        qrCodeUrl:
          typeof contact.qrCodeUrl === 'string' ? contact.qrCodeUrl : '',
        workHours:
          typeof contact.workHours === 'string' ? contact.workHours : '',
        wechatId: typeof contact.wechatId === 'string' ? contact.wechatId : '',
        extraNote:
          typeof contact.extraNote === 'string' ? contact.extraNote : '',
        enabled: contact.enabled !== false,
      }
    })
    .filter((item): item is SupportContact => Boolean(item))

  if (contacts.length > 0) {
    return contacts
  }

  const legacyQrCodeUrl =
    typeof parsed.qrCodeUrl === 'string' ? parsed.qrCodeUrl : ''
  if (!legacyQrCodeUrl) {
    return []
  }

  return [
    {
      id: 'support-default',
      label:
        typeof parsed.title === 'string' && parsed.title
          ? parsed.title
          : 'Official Support',
      description:
        typeof parsed.description === 'string' ? parsed.description : '',
      qrCodeUrl: legacyQrCodeUrl,
      workHours: typeof parsed.workHours === 'string' ? parsed.workHours : '',
      wechatId: typeof parsed.wechatId === 'string' ? parsed.wechatId : '',
      extraNote: typeof parsed.extraNote === 'string' ? parsed.extraNote : '',
      enabled: true,
    },
  ]
}

export function ClawXClientSection(props: ClawXClientSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const supportQRCodeInputRef = useRef<HTMLInputElement | null>(null)
  const [announcements, setAnnouncements] = useState<ClientAnnouncement[]>(() =>
    parseAnnouncements(props.announcementsData)
  )
  const [announcementsEnabled, setAnnouncementsEnabled] = useState(
    props.announcementsEnabled
  )
  const initialSupport = useMemo(
    () => parseSupport(props.supportData),
    [props.supportData]
  )
  const [supportContacts, setSupportContacts] = useState<SupportContact[]>(
    () => initialSupport.contacts
  )
  const [supportEnabled, setSupportEnabled] = useState(props.supportEnabled)
  const [selectedIds, setSelectedIds] = useState<string[]>([])
  const [selectedSupportContactIds, setSelectedSupportContactIds] = useState<
    string[]
  >([])
  const [hasAnnouncementChanges, setHasAnnouncementChanges] = useState(false)
  const [hasSupportChanges, setHasSupportChanges] = useState(false)
  const [showAnnouncementDialog, setShowAnnouncementDialog] = useState(false)
  const [showSupportContactDialog, setShowSupportContactDialog] =
    useState(false)
  const [showDeleteDialog, setShowDeleteDialog] = useState(false)
  const [showSupportDeleteDialog, setShowSupportDeleteDialog] = useState(false)
  const [editingAnnouncement, setEditingAnnouncement] =
    useState<ClientAnnouncement | null>(null)
  const [editingSupportContact, setEditingSupportContact] =
    useState<SupportContact | null>(null)
  const [supportQRCodeUploading, setSupportQRCodeUploading] = useState(false)

  const announcementForm = useForm<AnnouncementFormValues>({
    resolver: zodResolver(announcementSchema),
    defaultValues: {
      title: '',
      content: '',
      level: 'normal',
      publishedAt: new Date().toISOString(),
      expiresAt: '',
      link: '',
      enabled: true,
    },
  })

  const supportForm = useForm<SupportFormValues>({
    resolver: zodResolver(supportSchema),
    defaultValues: {
      title: initialSupport.title,
      description: initialSupport.description,
    },
  })

  const supportContactForm = useForm<SupportContactFormValues>({
    resolver: zodResolver(supportContactSchema),
    defaultValues: {
      label: '',
      description: '',
      qrCodeUrl: '',
      workHours: '',
      wechatId: '',
      extraNote: '',
      enabled: true,
    },
  })

  const sortedAnnouncements = useMemo(
    () =>
      [...announcements].sort(
        (a, b) =>
          new Date(b.publishedAt).getTime() - new Date(a.publishedAt).getTime()
      ),
    [announcements]
  )
  const supportChanged = hasSupportChanges || supportForm.formState.isDirty
  const watchedSupportQRCodeUrl = supportContactForm.watch('qrCodeUrl')

  const handleToggleAnnouncementsEnabled = async (checked: boolean) => {
    try {
      await updateOption.mutateAsync({
        key: 'clawx_client_setting.announcements_enabled',
        value: checked,
      })
      setAnnouncementsEnabled(checked)
      toast.success(t('Setting saved'))
    } catch {
      toast.error(t('Failed to update setting'))
    }
  }

  const handleToggleSupportEnabled = async (checked: boolean) => {
    try {
      await updateOption.mutateAsync({
        key: 'clawx_client_setting.support_enabled',
        value: checked,
      })
      setSupportEnabled(checked)
      toast.success(t('Setting saved'))
    } catch {
      toast.error(t('Failed to update setting'))
    }
  }

  const handleAddAnnouncement = () => {
    setEditingAnnouncement(null)
    announcementForm.reset({
      title: '',
      content: '',
      level: 'normal',
      publishedAt: new Date().toISOString(),
      expiresAt: '',
      link: '',
      enabled: true,
    })
    setShowAnnouncementDialog(true)
  }

  const handleEditAnnouncement = (announcement: ClientAnnouncement) => {
    setEditingAnnouncement(announcement)
    announcementForm.reset({
      title: announcement.title,
      content: announcement.content,
      level: announcement.level,
      publishedAt: announcement.publishedAt,
      expiresAt: announcement.expiresAt || '',
      link: announcement.link || '',
      enabled: announcement.enabled,
    })
    setShowAnnouncementDialog(true)
  }

  const handleAddSupportContact = () => {
    setEditingSupportContact(null)
    supportContactForm.reset({
      label: '',
      description: '',
      qrCodeUrl: '',
      workHours: '',
      wechatId: '',
      extraNote: '',
      enabled: true,
    })
    setShowSupportContactDialog(true)
  }

  const handleEditSupportContact = (contact: SupportContact) => {
    setEditingSupportContact(contact)
    supportContactForm.reset({
      label: contact.label,
      description: contact.description,
      qrCodeUrl: contact.qrCodeUrl,
      workHours: contact.workHours,
      wechatId: contact.wechatId,
      extraNote: contact.extraNote,
      enabled: contact.enabled,
    })
    setShowSupportContactDialog(true)
  }

  const handleSubmitAnnouncement = (values: AnnouncementFormValues) => {
    if (editingAnnouncement) {
      setAnnouncements((prev) =>
        prev.map((item) =>
          item.id === editingAnnouncement.id ? { ...item, ...values } : item
        )
      )
      toast.success(
        t('Client announcement updated. Click "Save Settings" to apply.')
      )
    } else {
      setAnnouncements((prev) => [
        ...prev,
        { id: makeAnnouncementId(), ...values },
      ])
      toast.success(
        t('Client announcement added. Click "Save Settings" to apply.')
      )
    }
    setHasAnnouncementChanges(true)
    setShowAnnouncementDialog(false)
  }

  const handleSubmitSupportContact = (values: SupportContactFormValues) => {
    if (editingSupportContact) {
      setSupportContacts((prev) =>
        prev.map((item) =>
          item.id === editingSupportContact.id ? { ...item, ...values } : item
        )
      )
      toast.success(
        t('Support contact updated. Click "Save Settings" to apply.')
      )
    } else {
      setSupportContacts((prev) => [
        ...prev,
        { id: makeSupportContactId(), ...values },
      ])
      toast.success(t('Support contact added. Click "Save Settings" to apply.'))
    }
    setHasSupportChanges(true)
    setShowSupportContactDialog(false)
  }

  const handleChooseSupportQRCode = () => {
    supportQRCodeInputRef.current?.click()
  }

  const handleUploadSupportQRCode = async (
    event: ChangeEvent<HTMLInputElement>
  ) => {
    const file = event.target.files?.[0]
    event.target.value = ''
    if (!file) {
      return
    }
    if (!['image/png', 'image/jpeg', 'image/gif'].includes(file.type)) {
      toast.error(t('Only PNG, JPG, or GIF images are supported.'))
      return
    }
    if (file.size > SUPPORT_QRCODE_MAX_SIZE) {
      toast.error(t('QR code image must be smaller than 2MB.'))
      return
    }
    setSupportQRCodeUploading(true)
    try {
      const result = await uploadClawXSupportQRCode(file)
      const url = result.data?.url
      if (!result.success || !url) {
        toast.error(result.message || t('Failed to upload QR code image'))
        return
      }
      supportContactForm.setValue('qrCodeUrl', url, {
        shouldDirty: true,
        shouldValidate: true,
      })
      toast.success(t('QR code image uploaded successfully'))
    } catch {
      toast.error(t('Failed to upload QR code image'))
    } finally {
      setSupportQRCodeUploading(false)
    }
  }

  const handleDeleteSelected = () => {
    if (selectedIds.length === 0) {
      toast.error(t('Please select items to delete'))
      return
    }
    setShowDeleteDialog(true)
  }

  const confirmDelete = () => {
    setAnnouncements((prev) =>
      prev.filter((item) => !selectedIds.includes(item.id))
    )
    setSelectedIds([])
    setHasAnnouncementChanges(true)
    setShowDeleteDialog(false)
    toast.success(
      t('Client announcement deleted. Click "Save Settings" to apply.')
    )
  }

  const handleDeleteSelectedSupportContacts = () => {
    if (selectedSupportContactIds.length === 0) {
      toast.error(t('Please select items to delete'))
      return
    }
    setShowSupportDeleteDialog(true)
  }

  const confirmDeleteSupportContacts = () => {
    setSupportContacts((prev) =>
      prev.filter((item) => !selectedSupportContactIds.includes(item.id))
    )
    setSelectedSupportContactIds([])
    setHasSupportChanges(true)
    setShowSupportDeleteDialog(false)
    toast.success(t('Support contact deleted. Click "Save Settings" to apply.'))
  }

  const handleSaveAnnouncements = async () => {
    try {
      await updateOption.mutateAsync({
        key: 'clawx_client_setting.announcements',
        value: JSON.stringify(announcements),
      })
      setHasAnnouncementChanges(false)
      toast.success(t('Client announcements saved successfully'))
    } catch {
      toast.error(t('Failed to save client announcements'))
    }
  }

  const handleSaveSupport = async (values: SupportFormValues) => {
    try {
      const payload: SupportConfig = {
        title: values.title,
        description: values.description,
        contacts: supportContacts,
      }
      await updateOption.mutateAsync({
        key: 'clawx_client_setting.support',
        value: JSON.stringify(payload),
      })
      setHasSupportChanges(false)
      supportForm.reset(values)
      toast.success(t('Support configuration saved successfully'))
    } catch {
      toast.error(t('Failed to save support configuration'))
    }
  }

  const toggleSelectAll = (checked: boolean) => {
    setSelectedIds(checked ? announcements.map((item) => item.id) : [])
  }

  const toggleSelectOne = (id: string, checked: boolean) => {
    setSelectedIds((prev) =>
      checked ? [...prev, id] : prev.filter((item) => item !== id)
    )
  }

  const toggleSelectAllSupportContacts = (checked: boolean) => {
    setSelectedSupportContactIds(
      checked ? supportContacts.map((item) => item.id) : []
    )
  }

  const toggleSelectSupportContact = (id: string, checked: boolean) => {
    setSelectedSupportContactIds((prev) =>
      checked ? [...prev, id] : prev.filter((item) => item !== id)
    )
  }

  return (
    <div className='space-y-4'>
      <SettingsSection title={t('ClawX Important Announcements')}>
        <div className='space-y-4'>
          <div className='flex flex-wrap items-center justify-between gap-2'>
            <div className='flex flex-wrap items-center gap-2'>
              <Button onClick={handleAddAnnouncement} size='sm'>
                <Plus className='mr-2 h-4 w-4' />
                {t('Add Client Announcement')}
              </Button>
              <Button
                onClick={handleDeleteSelected}
                size='sm'
                variant='destructive'
                disabled={selectedIds.length === 0}
              >
                <Trash2 className='mr-2 h-4 w-4' />
                {t('Delete (')}
                {selectedIds.length})
              </Button>
              <Button
                onClick={handleSaveAnnouncements}
                size='sm'
                variant='secondary'
                disabled={!hasAnnouncementChanges || updateOption.isPending}
              >
                <Save className='mr-2 h-4 w-4' />
                {updateOption.isPending ? t('Saving...') : t('Save Settings')}
              </Button>
            </div>
            <SettingsSwitchField
              checked={announcementsEnabled}
              onCheckedChange={handleToggleAnnouncementsEnabled}
              label={t('Enabled')}
              className='border-b-0 py-0'
            />
          </div>

          <StaticDataTable
            data={sortedAnnouncements}
            getRowKey={(announcement) => announcement.id}
            emptyContent={t('No client announcements yet.')}
            columns={[
              {
                id: 'select',
                header: (
                  <Checkbox
                    checked={
                      selectedIds.length === announcements.length &&
                      announcements.length > 0
                    }
                    onCheckedChange={toggleSelectAll}
                  />
                ),
                className: 'w-12',
                cell: (announcement) => (
                  <Checkbox
                    checked={selectedIds.includes(announcement.id)}
                    onCheckedChange={(checked) =>
                      toggleSelectOne(announcement.id, checked as boolean)
                    }
                  />
                ),
              },
              {
                id: 'title',
                header: t('Title'),
                cellClassName: 'max-w-xs truncate',
                cell: (announcement) => announcement.title,
              },
              {
                id: 'level',
                header: t('Level'),
                cell: (announcement) => {
                  const option = levelOptions.find(
                    (item) => item.value === announcement.level
                  )
                  return (
                    <StatusBadge
                      label={option ? t(option.label) : announcement.level}
                      variant={option?.badgeVariant ?? 'neutral'}
                      copyable={false}
                    />
                  )
                },
              },
              {
                id: 'published-at',
                header: t('Publish Date'),
                cell: (announcement) => (
                  <span className='text-sm'>
                    {dayjs(announcement.publishedAt).format('YYYY-MM-DD HH:mm')}
                  </span>
                ),
              },
              {
                id: 'status',
                header: t('Status'),
                cell: (announcement) => (
                  <StatusBadge
                    label={announcement.enabled ? t('Enabled') : t('Disabled')}
                    variant={announcement.enabled ? 'success' : 'neutral'}
                    copyable={false}
                  />
                ),
              },
              {
                id: 'actions',
                header: t('Actions'),
                className: 'w-24',
                cell: (announcement) => (
                  <Button
                    onClick={() => handleEditAnnouncement(announcement)}
                    size='sm'
                    variant='ghost'
                  >
                    <Edit className='h-4 w-4' />
                  </Button>
                ),
              },
            ]}
          />
        </div>
      </SettingsSection>

      <SettingsSection title={t('ClawX Support Contact')}>
        <div className='space-y-4'>
          <Form {...supportForm}>
            <form
              id={SUPPORT_FORM_ID}
              onSubmit={supportForm.handleSubmit(handleSaveSupport)}
              className='grid gap-4 lg:grid-cols-2'
            >
              <FormField
                control={supportForm.control}
                name='title'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Title')}</FormLabel>
                    <FormControl>
                      <Input placeholder={t('Help & Support')} {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={supportForm.control}
                name='description'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Description')}</FormLabel>
                    <FormControl>
                      <Textarea
                        rows={3}
                        placeholder={t(
                          'Scan the QR code to contact support for account, billing, and model usage questions.'
                        )}
                        {...field}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </form>
          </Form>

          <div className='flex flex-wrap items-center justify-between gap-2'>
            <div className='flex flex-wrap items-center gap-2'>
              <Button onClick={handleAddSupportContact} size='sm'>
                <Plus className='mr-2 h-4 w-4' />
                {t('Add Support Contact')}
              </Button>
              <Button
                onClick={handleDeleteSelectedSupportContacts}
                size='sm'
                variant='destructive'
                disabled={selectedSupportContactIds.length === 0}
              >
                <Trash2 className='mr-2 h-4 w-4' />
                {t('Delete (')}
                {selectedSupportContactIds.length})
              </Button>
              <Button
                type='submit'
                form={SUPPORT_FORM_ID}
                size='sm'
                variant='secondary'
                disabled={!supportChanged || updateOption.isPending}
              >
                <Save className='mr-2 h-4 w-4' />
                {updateOption.isPending ? t('Saving...') : t('Save Settings')}
              </Button>
            </div>
            <SettingsSwitchField
              checked={supportEnabled}
              onCheckedChange={handleToggleSupportEnabled}
              label={t('Enabled')}
              description={t(
                'Show the Help & Support entry when at least one enabled contact has a QR code.'
              )}
              className='border-b-0 py-0'
            />
          </div>

          <StaticDataTable
            data={supportContacts}
            getRowKey={(contact) => contact.id}
            emptyContent={t('No support contacts yet.')}
            columns={[
              {
                id: 'select',
                header: (
                  <Checkbox
                    checked={
                      selectedSupportContactIds.length ===
                        supportContacts.length && supportContacts.length > 0
                    }
                    onCheckedChange={toggleSelectAllSupportContacts}
                  />
                ),
                className: 'w-12',
                cell: (contact) => (
                  <Checkbox
                    checked={selectedSupportContactIds.includes(contact.id)}
                    onCheckedChange={(checked) =>
                      toggleSelectSupportContact(contact.id, checked as boolean)
                    }
                  />
                ),
              },
              {
                id: 'label',
                header: t('Name'),
                cellClassName: 'max-w-xs truncate',
                cell: (contact) => contact.label,
              },
              {
                id: 'wechat-id',
                header: t('WeChat ID'),
                cellClassName: 'max-w-xs truncate',
                cell: (contact) => contact.wechatId || '-',
              },
              {
                id: 'qr-code',
                header: t('QR Code'),
                cellClassName: 'max-w-xs truncate',
                cell: (contact) => contact.qrCodeUrl,
              },
              {
                id: 'status',
                header: t('Status'),
                cell: (contact) => (
                  <StatusBadge
                    label={contact.enabled ? t('Enabled') : t('Disabled')}
                    variant={contact.enabled ? 'success' : 'neutral'}
                    copyable={false}
                  />
                ),
              },
              {
                id: 'actions',
                header: t('Actions'),
                className: 'w-24',
                cell: (contact) => (
                  <Button
                    onClick={() => handleEditSupportContact(contact)}
                    size='sm'
                    variant='ghost'
                  >
                    <Edit className='h-4 w-4' />
                  </Button>
                ),
              },
            ]}
          />
          <p className='text-muted-foreground text-sm'>
            {t(
              'The ClawX client hides this entry when support is disabled or no enabled contact has a QR code.'
            )}
          </p>
        </div>
      </SettingsSection>

      <Dialog
        open={showAnnouncementDialog}
        onOpenChange={setShowAnnouncementDialog}
        title={
          editingAnnouncement
            ? t('Edit Client Announcement')
            : t('Add Client Announcement')
        }
        description={t('Create important announcements displayed in ClawX.')}
        contentClassName='max-w-2xl'
        contentHeight='auto'
        bodyClassName='space-y-4'
        footer={
          <>
            <Button
              type='button'
              variant='outline'
              onClick={() => setShowAnnouncementDialog(false)}
            >
              {t('Cancel')}
            </Button>
            <Button type='submit' form={ANNOUNCEMENT_FORM_ID}>
              {editingAnnouncement ? t('Update') : t('Add')}
            </Button>
          </>
        }
      >
        <Form {...announcementForm}>
          <form
            id={ANNOUNCEMENT_FORM_ID}
            onSubmit={announcementForm.handleSubmit(handleSubmitAnnouncement)}
            className='space-y-4'
          >
            <FormField
              control={announcementForm.control}
              name='title'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Title')}</FormLabel>
                  <FormControl>
                    <Input placeholder={t('Announcement title')} {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={announcementForm.control}
              name='content'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Content')}</FormLabel>
                  <FormControl>
                    <Textarea
                      rows={5}
                      placeholder={t('Announcement content')}
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Keep announcements concise. The ClawX client displays plain text.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <div className='grid gap-4 lg:grid-cols-2'>
              <FormField
                control={announcementForm.control}
                name='level'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Level')}</FormLabel>
                    <Select
                      items={levelOptions.map((option) => ({
                        value: option.value,
                        label: t(option.label),
                      }))}
                      onValueChange={field.onChange}
                      value={field.value}
                    >
                      <FormControl>
                        <SelectTrigger>
                          <SelectValue placeholder={t('Select level')} />
                        </SelectTrigger>
                      </FormControl>
                      <SelectContent alignItemWithTrigger={false}>
                        <SelectGroup>
                          {levelOptions.map((option) => (
                            <SelectItem key={option.value} value={option.value}>
                              {t(option.label)}
                            </SelectItem>
                          ))}
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={announcementForm.control}
                name='enabled'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Status')}</FormLabel>
                    <FormControl>
                      <Checkbox
                        checked={field.value}
                        onCheckedChange={(checked) =>
                          field.onChange(Boolean(checked))
                        }
                      />
                    </FormControl>
                    <FormDescription>{t('Enabled')}</FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>
            <div className='grid gap-4 lg:grid-cols-2'>
              <FormField
                control={announcementForm.control}
                name='publishedAt'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Publish Date')}</FormLabel>
                    <FormControl>
                      <DateTimePicker
                        value={field.value ? new Date(field.value) : undefined}
                        onChange={(date) =>
                          field.onChange(date ? date.toISOString() : '')
                        }
                        placeholder={t('Select publish date')}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={announcementForm.control}
                name='expiresAt'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Expires At')}</FormLabel>
                    <FormControl>
                      <DateTimePicker
                        value={field.value ? new Date(field.value) : undefined}
                        onChange={(date) =>
                          field.onChange(date ? date.toISOString() : '')
                        }
                        placeholder={t('Optional expiration date')}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>
            <FormField
              control={announcementForm.control}
              name='link'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Link')}</FormLabel>
                  <FormControl>
                    <Input placeholder='https://example.com' {...field} />
                  </FormControl>
                  <FormDescription>
                    {t('Optional external link opened from the ClawX client.')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </form>
        </Form>
      </Dialog>

      <Dialog
        open={showSupportContactDialog}
        onOpenChange={setShowSupportContactDialog}
        title={
          editingSupportContact
            ? t('Edit Support Contact')
            : t('Add Support Contact')
        }
        description={t(
          'Configure one QR code contact shown in the ClawX client.'
        )}
        contentClassName='max-w-2xl'
        contentHeight='auto'
        bodyClassName='space-y-4'
        footer={
          <>
            <Button
              type='button'
              variant='outline'
              onClick={() => setShowSupportContactDialog(false)}
            >
              {t('Cancel')}
            </Button>
            <Button type='submit' form={SUPPORT_CONTACT_FORM_ID}>
              {editingSupportContact ? t('Update') : t('Add')}
            </Button>
          </>
        }
      >
        <Form {...supportContactForm}>
          <form
            id={SUPPORT_CONTACT_FORM_ID}
            onSubmit={supportContactForm.handleSubmit(
              handleSubmitSupportContact
            )}
            className='space-y-4'
          >
            <div className='grid gap-4 lg:grid-cols-2'>
              <FormField
                control={supportContactForm.control}
                name='label'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Name')}</FormLabel>
                    <FormControl>
                      <Input placeholder={t('Official Support')} {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={supportContactForm.control}
                name='qrCodeUrl'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('WeCom QR Code URL')}</FormLabel>
                    <div className='flex flex-col gap-2 sm:flex-row'>
                      <FormControl>
                        <Input
                          placeholder='https://example.com/support-qrcode.png'
                          {...field}
                        />
                      </FormControl>
                      <input
                        ref={supportQRCodeInputRef}
                        type='file'
                        accept={SUPPORT_QRCODE_ACCEPT}
                        className='hidden'
                        onChange={handleUploadSupportQRCode}
                      />
                      <Button
                        type='button'
                        variant='outline'
                        onClick={handleChooseSupportQRCode}
                        disabled={supportQRCodeUploading}
                        className='shrink-0'
                      >
                        {supportQRCodeUploading ? (
                          <Loader2 className='mr-2 h-4 w-4 animate-spin' />
                        ) : (
                          <ImageUp className='mr-2 h-4 w-4' />
                        )}
                        {supportQRCodeUploading
                          ? t('Uploading...')
                          : t('Upload')}
                      </Button>
                    </div>
                    <FormDescription>
                      {t(
                        'Upload a QR code image or paste an image URL. PNG, JPG, and GIF are supported, up to 2MB.'
                      )}
                    </FormDescription>
                    {watchedSupportQRCodeUrl ? (
                      <div className='mt-2 inline-flex rounded-lg border bg-white p-2'>
                        <img
                          src={watchedSupportQRCodeUrl}
                          alt={t('Support QR code preview')}
                          className='h-28 w-28 object-contain'
                        />
                      </div>
                    ) : null}
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>
            <div className='grid gap-4 lg:grid-cols-2'>
              <FormField
                control={supportContactForm.control}
                name='workHours'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Service Hours')}</FormLabel>
                    <FormControl>
                      <Input
                        placeholder={t('Weekdays 9:00-18:00')}
                        {...field}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={supportContactForm.control}
                name='wechatId'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('WeChat ID')}</FormLabel>
                    <FormControl>
                      <Input placeholder='support' {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>
            <FormField
              control={supportContactForm.control}
              name='description'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Description')}</FormLabel>
                  <FormControl>
                    <Textarea
                      rows={3}
                      placeholder={t(
                        'Scan the QR code to contact support for account, billing, and model usage questions.'
                      )}
                      {...field}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={supportContactForm.control}
              name='extraNote'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Extra Notes')}</FormLabel>
                  <FormControl>
                    <Input
                      placeholder={t('Support will reply as soon as possible.')}
                      {...field}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={supportContactForm.control}
              name='enabled'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Status')}</FormLabel>
                  <FormControl>
                    <Checkbox
                      checked={field.value}
                      onCheckedChange={(checked) =>
                        field.onChange(Boolean(checked))
                      }
                    />
                  </FormControl>
                  <FormDescription>{t('Enabled')}</FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </form>
        </Form>
      </Dialog>

      <AlertDialog open={showDeleteDialog} onOpenChange={setShowDeleteDialog}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Are you sure?')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                '{{count}} client announcements will be removed from the list.',
                {
                  count: selectedIds.length,
                }
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
            <AlertDialogAction onClick={confirmDelete}>
              {t('Delete')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog
        open={showSupportDeleteDialog}
        onOpenChange={setShowSupportDeleteDialog}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Are you sure?')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t('{{count}} support contacts will be removed from the list.', {
                count: selectedSupportContactIds.length,
              })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
            <AlertDialogAction onClick={confirmDeleteSupportContacts}>
              {t('Delete')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}

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

export type ClawXReleasePlatform = 'mac' | 'win' | 'linux'
export type ClawXReleasePackageType = 'installer' | 'portable_zip'

export type ClawXRelease = {
  id: number
  channel: string
  platform: ClawXReleasePlatform
  arch: string
  package_type: ClawXReleasePackageType
  version: string
  file_name: string
  file_url: string
  sha512: string
  size: number
  release_date: string
  release_notes: string
  enabled: boolean
  mandatory: boolean
  created_at: number
  updated_at: number
}

export type ClawXReleaseFormData = Omit<
  ClawXRelease,
  'id' | 'created_at' | 'updated_at'
>

export type ApiResponse<T = unknown> = {
  success: boolean
  message?: string
  data?: T
}

export type GetClawXReleasesResponse = ApiResponse<{
  items: ClawXRelease[]
  total: number
  page: number
  page_size: number
}>

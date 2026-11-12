param(
    [Parameter(Mandatory = $true)]
    [string]$ExePath,

    [Parameter(Mandatory = $true)]
    [string]$IconPath
)

$ErrorActionPreference = 'Stop'

if (-not ('AuraluxIconResourceWriter' -as [type])) {
Add-Type -TypeDefinition @'
using System;
using System.ComponentModel;
using System.IO;
using System.Runtime.InteropServices;

public static class AuraluxIconResourceWriter
{
    private const ushort RT_ICON = 3;
    private const ushort RT_GROUP_ICON = 14;
    private const ushort APP_ICON_GROUP_ID = 1;
    private const ushort FIRST_IMAGE_ID = 101;

    [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
    private static extern IntPtr BeginUpdateResourceW(string fileName, bool deleteExistingResources);

    [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
    private static extern bool UpdateResourceW(
        IntPtr update,
        IntPtr type,
        IntPtr name,
        ushort language,
        byte[] data,
        uint dataSize);

    [DllImport("kernel32.dll", SetLastError = true)]
    private static extern bool EndUpdateResourceW(IntPtr update, bool discard);

    private static IntPtr ResourceId(ushort value)
    {
        return new IntPtr(value);
    }

    private sealed class IconImage
    {
        public byte Width;
        public byte Height;
        public byte ColorCount;
        public byte Reserved;
        public ushort Planes;
        public ushort BitCount;
        public uint Size;
        public byte[] Data;
    }

    public static void Write(string exePath, string iconPath)
    {
        IconImage[] images = ReadIcon(iconPath);
        IntPtr update = BeginUpdateResourceW(exePath, false);
        if (update == IntPtr.Zero)
            throw new Win32Exception(Marshal.GetLastWin32Error(), "Could not open EXE resources");

        bool committed = false;
        try
        {
            for (int i = 0; i < images.Length; i++)
            {
                ushort imageId = checked((ushort)(FIRST_IMAGE_ID + i));
                if (!UpdateResourceW(update, ResourceId(RT_ICON), ResourceId(imageId), 0,
                    images[i].Data, (uint)images[i].Data.Length))
                    throw new Win32Exception(Marshal.GetLastWin32Error(), "Could not write an icon image resource");
            }

            byte[] group = BuildGroup(images);
            if (!UpdateResourceW(update, ResourceId(RT_GROUP_ICON), ResourceId(APP_ICON_GROUP_ID), 0,
                group, (uint)group.Length))
                throw new Win32Exception(Marshal.GetLastWin32Error(), "Could not write the application icon group resource");

            if (!EndUpdateResourceW(update, false))
                throw new Win32Exception(Marshal.GetLastWin32Error(), "Could not commit EXE icon resources");
            committed = true;
        }
        finally
        {
            if (!committed)
                EndUpdateResourceW(update, true);
        }
    }

    private static IconImage[] ReadIcon(string path)
    {
        byte[] bytes = File.ReadAllBytes(path);
        using (BinaryReader reader = new BinaryReader(new MemoryStream(bytes, false)))
        {
            if (bytes.Length < 6 || reader.ReadUInt16() != 0 || reader.ReadUInt16() != 1)
                throw new InvalidDataException("The input file is not a valid Windows ICO");

            ushort count = reader.ReadUInt16();
            if (count == 0 || count > 100 || bytes.Length < 6 + count * 16)
                throw new InvalidDataException("The ICO image directory is invalid");

            IconImage[] images = new IconImage[count];
            for (int i = 0; i < count; i++)
            {
                IconImage image = new IconImage();
                image.Width = reader.ReadByte();
                image.Height = reader.ReadByte();
                image.ColorCount = reader.ReadByte();
                image.Reserved = reader.ReadByte();
                image.Planes = reader.ReadUInt16();
                image.BitCount = reader.ReadUInt16();
                image.Size = reader.ReadUInt32();
                uint offset = reader.ReadUInt32();
                if (image.Size == 0 || offset > bytes.Length || image.Size > bytes.Length - offset)
                    throw new InvalidDataException("ICO image data is outside the file bounds");
                image.Data = new byte[image.Size];
                Buffer.BlockCopy(bytes, checked((int)offset), image.Data, 0, checked((int)image.Size));
                images[i] = image;
            }
            return images;
        }
    }

    private static byte[] BuildGroup(IconImage[] images)
    {
        using (MemoryStream stream = new MemoryStream(6 + images.Length * 14))
        using (BinaryWriter writer = new BinaryWriter(stream))
        {
            writer.Write((ushort)0);
            writer.Write((ushort)1);
            writer.Write((ushort)images.Length);
            for (int i = 0; i < images.Length; i++)
            {
                IconImage image = images[i];
                writer.Write(image.Width);
                writer.Write(image.Height);
                writer.Write(image.ColorCount);
                writer.Write(image.Reserved);
                writer.Write(image.Planes);
                writer.Write(image.BitCount);
                writer.Write(image.Size);
                writer.Write(checked((ushort)(FIRST_IMAGE_ID + i)));
            }
            return stream.ToArray();
        }
    }
}
'@
}

$resolvedExe = (Resolve-Path -LiteralPath $ExePath).Path
$resolvedIcon = (Resolve-Path -LiteralPath $IconPath).Path
[AuraluxIconResourceWriter]::Write($resolvedExe, $resolvedIcon)
Write-Host "Embedded application icon: $resolvedIcon"

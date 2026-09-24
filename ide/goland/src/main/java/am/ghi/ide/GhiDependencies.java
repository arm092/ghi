package am.ghi.ide;

import com.google.gson.*;
import com.intellij.openapi.vfs.*;
import com.intellij.openapi.roots.ProjectFileIndex;
import com.intellij.psi.*;
import java.io.IOException;
import java.util.*;
import java.util.regex.Pattern;

/** Read-only, lock-scoped Ghi sources. Integrity checks remain the compiler's job. */
final class GhiDependencies {
    private static final Pattern NAMESPACE=Pattern.compile("[A-Za-z_][A-Za-z0-9_]*(\\.[A-Za-z_][A-Za-z0-9_]*)*");
    private static final Set<String> SKIP=Set.of("bin","vendor","node_modules");

    static VirtualFile root(PsiFile context){
        VirtualFile file=context.getOriginalFile().getVirtualFile();
        if(file==null)return null;
        // Package sources may contain their own manifest. They belong to the
        // consuming application's installed tree, not a second IDE project.
        for(VirtualFile at=file.getParent();at!=null;at=at.getParent())if(at.getName().equals(".ghi")&&at.getParent()!=null
            &&at.findChild("packages")!=null){
            VirtualFile owner=at.getParent();
            return owner.findChild("mojave.lock")!=null?owner:null;
        }
        VirtualFile content=ProjectFileIndex.getInstance(context.getProject()).getContentRootForFile(file);
        String base=content==null?context.getProject().getBasePath():content.getPath();
        if(base==null)return null;
        String boundary=base.replace('\\','/');
        for(VirtualFile dir=file.getParent();dir!=null;dir=dir.getParent()){
            if(!within(dir.getPath(),boundary))break;
            if(dir.findChild("mojave.lock")!=null)return dir;
            if(dir.findChild("mojave.json")!=null)return null;
        }
        return null;
    }

    static VirtualFile applicationRoot(PsiFile context){
        VirtualFile locked=root(context);
        if(locked!=null)return locked;
        VirtualFile file=context.getOriginalFile().getVirtualFile();
        if(file==null)return null;
        VirtualFile content=ProjectFileIndex.getInstance(context.getProject()).getContentRootForFile(file);
        for(VirtualFile dir=file.getParent();dir!=null;dir=dir.getParent()){
            if(dir.findChild("mojave.json")!=null||dir.findChild("mojave.lock")!=null)return dir;
            if(dir==content)break;
        }
        return null;
    }

    static boolean installed(PsiFile file){
        VirtualFile vf=file.getVirtualFile();
        return vf!=null&&vf.getPath().replace('\\','/').contains("/.ghi/packages/");
    }

    static boolean generated(VirtualFile file){
        for(VirtualFile at=file;at!=null;at=at.getParent())if(at.getName().equals(".ghi"))return true;
        return false;
    }

    static boolean inApplication(VirtualFile file,VirtualFile root){
        if(!within(file.getPath(),root.getPath()))return false;
        for(VirtualFile dir=file.getParent();dir!=null&&dir!=root;dir=dir.getParent())
            if(dir.findChild("mojave.json")!=null||dir.findChild("mojave.lock")!=null)return false;
        return true;
    }

    static void add(PsiFile context,GhiSymbols model){
        VirtualFile projectRoot=root(context);
        if(projectRoot==null)return;
        VirtualFile lock=projectRoot.findChild("mojave.lock");
        if(lock==null||lock.isDirectory())return;
        try{
            JsonObject document=JsonParser.parseString(VfsUtilCore.loadText(lock)).getAsJsonObject();
            if(document.get("version").getAsInt()!=1)return;
            JsonArray packages=document.getAsJsonArray("packages");
            if(packages==null)return;
            VirtualFile hidden=projectRoot.findChild(".ghi");
            VirtualFile cache=hidden==null?null:hidden.findChild("packages");
            if(cache==null||!cache.isDirectory()||hidden.is(VFileProperty.SYMLINK)||cache.is(VFileProperty.SYMLINK))return;
            Set<String> seen=new HashSet<>();
            for(JsonElement item:packages){
                String namespace=item.getAsJsonObject().get("namespace").getAsString();
                if(!NAMESPACE.matcher(namespace).matches()||!seen.add(namespace.toLowerCase(Locale.ROOT)))return;
            }
            for(JsonElement item:packages){
                String namespace=item.getAsJsonObject().get("namespace").getAsString();
                VirtualFile directory=cache.findChild(namespace);
                if(directory!=null&&directory.isDirectory()&&!directory.is(VFileProperty.SYMLINK))scan(context,directory,directory,namespace,model);
            }
        }catch(IOException|JsonParseException|IllegalStateException|IllegalArgumentException|NullPointerException|UnsupportedOperationException ignored){
            // Incomplete lock updates give no dependency candidates.
        }
    }

    private static void scan(PsiFile context,VirtualFile root,VirtualFile directory,String namespace,GhiSymbols model){
        for(VirtualFile child:directory.getChildren()){
            if(child.is(VFileProperty.SYMLINK))continue;
            if(child.isDirectory()){
                if(child.getName().startsWith(".")||SKIP.contains(child.getName())||directory==root&&child.getName().equals("tests"))continue;
                scan(context,root,child,namespace,model);
            }else if(child.getName().endsWith(".ghi")){
                PsiFile source=PsiManager.getInstance(context.getProject()).findFile(child);
                if(source==null||model.sources.containsKey(source))continue;
                model.addDependency(source,namespace);
            }
        }
    }

    private static boolean within(String path,String root){return path.equals(root)||path.startsWith(root+"/");}
}
